package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// cursorOfficialActive 在首次成功拉到官方用量后置 true。
// MITM 侧 (proxy.go 的 cursor opaque 估算) 看到此标志后会跳过估算，
// 避免「精确官方数据 + 体积估算」双计。降级（连续失败）会把标志清回 false。
var cursorOfficialActive atomic.Bool

// CursorOfficialSyncActive 给 MITM 估算路径判断是否应该让位给官方数据。
func CursorOfficialSyncActive() bool { return cursorOfficialActive.Load() }

// cursorSyncState 持久化游标，避免重启后重新拉历史。
type cursorSyncState struct {
	LastSyncedMs int64  `json:"last_synced_ms"`
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	UpdatedAt    string `json:"updated_at"`
}

func cursorSyncStatePath() string {
	return filepath.Join(appDataDir(), "cursor_sync_state.json")
}

func loadCursorSyncState() cursorSyncState {
	var s cursorSyncState
	b, err := os.ReadFile(cursorSyncStatePath())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

func saveCursorSyncState(s cursorSyncState) {
	dir := appDataDir()
	_ = os.MkdirAll(dir, 0o755)
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	tmp := cursorSyncStatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, cursorSyncStatePath())
}

// StartCursorOfficialSync 后台同步 Cursor 官方用量。
// 30s 内首次拉取；之后每 10 分钟一次。
// 任何阶段失败都静默重试，连续 6 次（约 1 小时）失败后清掉 active 标志降级。
func StartCursorOfficialSync(ctx context.Context, reporter *Reporter) {
	if reporter == nil {
		return
	}
	go func() {
		// 启动延迟，让 reporter / 网络栈就绪。
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}

		session, err := LoadCursorSession(ctx)
		if err != nil {
			log.Printf("[cursor-sync] 未启用：%v（保持 MITM 估算）", err)
			return
		}
		log.Printf("[cursor-sync] 启用官方用量同步 email=%s membership=%s userID=%s",
			redactEmail(session.Email), session.Membership, session.UserID)

		client := NewCursorOfficialClient(session)
		state := loadCursorSyncState()
		// 不同用户登录时把游标清掉，避免拿别人 ts 当起点。
		if state.UserID != "" && state.UserID != session.UserID {
			state = cursorSyncState{}
		}
		state.UserID = session.UserID
		state.Email = session.Email

		// 首次启动若无游标，从「24 小时前」开始，足够覆盖当日仪表盘。
		if state.LastSyncedMs == 0 {
			state.LastSyncedMs = time.Now().Add(-24 * time.Hour).UnixMilli()
		}

		const maxFail = 6
		failCount := 0
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		// 立即拉一次。
		runOnce := func() {
			fetchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			events, err := client.FetchUsageEventsSince(fetchCtx, state.LastSyncedMs, 20)
			if err != nil {
				failCount++
				log.Printf("[cursor-sync] 拉取失败 (%d/%d): %v", failCount, maxFail, err)
				if failCount >= maxFail && cursorOfficialActive.Load() {
					cursorOfficialActive.Store(false)
					log.Printf("[cursor-sync] 连续失败，降级回 MITM 估算")
				}
				return
			}
			failCount = 0
			if len(events) == 0 {
				cursorOfficialActive.Store(true)
				return
			}

			var newest int64 = state.LastSyncedMs
			added := 0
			for _, evt := range events {
				ts := evt.TimestampMs()
				if ts > newest {
					newest = ts
				}
				rec := cursorEventToRecord(evt)
				if rec == nil {
					continue
				}
				reporter.Add(*rec)
				added++
			}
			if newest > state.LastSyncedMs {
				state.LastSyncedMs = newest
				saveCursorSyncState(state)
			}
			cursorOfficialActive.Store(true)
			log.Printf("[cursor-sync] 同步 %d 条事件（共拉 %d 条，游标 %s）",
				added, len(events), time.UnixMilli(newest).Format(time.RFC3339))
		}

		runOnce()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOnce()
			}
		}
	}()
}

// cursorEventToRecord 把 Cursor 官方事件映射成 UsageRecord。
// 仅保留有 token 计费的事件；纯请求计数（订阅 quota）跳过，避免与 token 列双计。
func cursorEventToRecord(evt CursorUsageEvent) *UsageRecord {
	tu := evt.TokenUsage
	total := tu.InputTokens + tu.OutputTokens
	if total <= 0 {
		// 无 token 信息的事件（例如纯订阅计数）暂不上报，否则会污染 token 列。
		return nil
	}
	ts := evt.TimestampTime()
	if ts.IsZero() {
		// 时间戳解析失败的事件丢弃，避免写入 0001-01-01 污染日报。
		return nil
	}
	model := strings.TrimSpace(evt.Model)
	if model == "" {
		model = "cursor-unknown"
	}
	// RequestID 用 (timestamp, model) 拼装，配合 backend (request_id, source) 去重键。
	reqID := fmt.Sprintf("cursor-evt-%s-%s", evt.Timestamp, model)
	rec := &UsageRecord{
		Vendor:              "cursor",
		Model:               model,
		Endpoint:            "cursor-official/" + strings.TrimSpace(evt.Kind),
		PromptTokens:        int(tu.InputTokens),
		CompletionTokens:    int(tu.OutputTokens),
		TotalTokens:         int(total),
		CacheReadTokens:     int(tu.CacheReadTokens),
		CacheCreationTokens: int(tu.CacheWriteTokens),
		RequestID:           reqID,
		Source:              "cursor-official-api",
		RequestTime:         ts.Format(time.RFC3339),
		SourceKind:          "official",
		Accuracy:            "exact",
		MergeStatus:         "unmatched",
	}
	return rec
}

func redactEmail(e string) string {
	at := strings.IndexByte(e, '@')
	if at <= 1 {
		return e
	}
	return e[:1] + "***" + e[at:]
}
