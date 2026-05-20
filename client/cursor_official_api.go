package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CursorOfficialClient 通过 Cursor 桌面后端 (api2.cursor.sh) 的 Connect-RPC
// 拉取权威用量数据。与 MITM 估算相比，这里拿到的是 Cursor 自己 fan-out 到
// 计费侧的精确 token / 美分成本，按事件级别返回。
type CursorOfficialClient struct {
	httpClient *http.Client
	baseURL    string
	session    *CursorSession
}

func NewCursorOfficialClient(session *CursorSession) *CursorOfficialClient {
	return &CursorOfficialClient{
		// 显式 Proxy: nil 避开 ai-monitor 自身的 MITM 代理，
		// 否则 cursor 流量会被自己拦下来再估算一遍，绕成死循环。
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				Proxy:                 nil,
				MaxIdleConns:          10,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
			},
		},
		baseURL: "https://api2.cursor.sh",
		session: session,
	}
}

// CursorUsageEvent 来自 GetFilteredUsageEvents 的单条事件。
// 字段名与 Cursor 协议对齐；timestamp 是毫秒字符串。
type CursorUsageEvent struct {
	Timestamp        string  `json:"timestamp"`
	Model            string  `json:"model"`
	Kind             string  `json:"kind"`
	RequestsCosts    float64 `json:"requestsCosts"`
	IsTokenBasedCall bool    `json:"isTokenBasedCall"`
	IsChargeable     bool    `json:"isChargeable"`
	ChargedCents     float64 `json:"chargedCents"`
	OwningUser       string  `json:"owningUser"`
	TokenUsage       struct {
		InputTokens      int64   `json:"inputTokens"`
		OutputTokens     int64   `json:"outputTokens"`
		CacheReadTokens  int64   `json:"cacheReadTokens"`
		CacheWriteTokens int64   `json:"cacheWriteTokens"`
		TotalCents       float64 `json:"totalCents"`
	} `json:"tokenUsage"`
}

// TimestampMs 把字符串毫秒时间戳解析为 int64；解析失败返回 0。
func (e CursorUsageEvent) TimestampMs() int64 {
	v, _ := strconv.ParseInt(e.Timestamp, 10, 64)
	return v
}

// TimestampTime 把毫秒时间戳转成 time.Time（UTC）。
func (e CursorUsageEvent) TimestampTime() time.Time {
	ms := e.TimestampMs()
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

type filteredUsageEventsResponse struct {
	TotalUsageEventsCount int                `json:"totalUsageEventsCount"`
	UsageEventsDisplay    []CursorUsageEvent `json:"usageEventsDisplay"`
}

// FetchUsageEventsSince 从最新一页向后翻，直到遇到时间戳 ≤ sinceMs 的事件就停。
// 用于增量同步：调用方持久化 lastSyncedTs，本次只拿增量。
// 同一毫秒内可能有多条事件，调用方 dedup 用 (timestamp, model) 做键。
func (c *CursorOfficialClient) FetchUsageEventsSince(ctx context.Context, sinceMs int64, maxPages int) ([]CursorUsageEvent, error) {
	const pageSize = 100
	var result []CursorUsageEvent
	for page := 1; page <= maxPages; page++ {
		body := fmt.Sprintf(`{"pageIndex":%d,"pageSize":%d}`, page, pageSize)
		var resp filteredUsageEventsResponse
		if err := c.callConnect(ctx, "/aiserver.v1.DashboardService/GetFilteredUsageEvents", []byte(body), &resp); err != nil {
			return result, err
		}
		if len(resp.UsageEventsDisplay) == 0 {
			break
		}
		stop := false
		for _, evt := range resp.UsageEventsDisplay {
			if evt.TimestampMs() <= sinceMs {
				stop = true
				break
			}
			result = append(result, evt)
		}
		if stop || len(resp.UsageEventsDisplay) < pageSize {
			break
		}
	}
	return result, nil
}

// callConnect 实现一个最小化的 Connect-RPC unary 调用：JSON 编解码、
// Connect-Protocol-Version: 1、Bearer 鉴权。失败时打回结构化错误。
func (c *CursorOfficialClient) callConnect(ctx context.Context, path string, body []byte, out interface{}) error {
	if c.session == nil || strings.TrimSpace(c.session.AccessToken) == "" {
		return fmt.Errorf("no cursor session")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.session.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	// 模仿 Cursor 桌面客户端的 UA，避免被对方策略性限流。
	req.Header.Set("User-Agent", "Cursor/ai-monitor (+https://cursor.com)")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode != http.StatusOK {
		// 把错误体的前 200 字节带回去，便于运维排查 401/403/版本漂移。
		snippet := string(raw)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return fmt.Errorf("cursor %s status=%d body=%s", path, resp.StatusCode, snippet)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
