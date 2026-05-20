package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// CursorSession 是从本机 Cursor 安装目录读出的当前登录会话。
// 仅含调用 Cursor 官方用量 API 所需的字段。
type CursorSession struct {
	AccessToken string // JWT，可直接作为 Bearer 使用
	UserID      string // JWT 的 sub 字段（cursor.com/api/usage?user= 用得到）
	Email       string // 缓存邮箱，仅用于日志展示
	Membership  string // pro / business / free 等
}

// resolveCursorStateDBPath 给出 Cursor globalStorage state.vscdb 的本机路径。
// 不存在或路径不可解析时返回空串与错误。
func resolveCursorStateDBPath() (string, error) {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA 未设置")
		}
		return filepath.Join(appData, "Cursor", "User", "globalStorage", "state.vscdb"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"), nil
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"), nil
	default:
		return "", fmt.Errorf("不支持的平台 %s", runtime.GOOS)
	}
}

// copyToTempReadonly 把 state.vscdb 拷一份到临时目录。
// 直接打开原文件会和 Cursor 自身的 WAL 写入抢锁，偶发 SQLITE_BUSY；
// 同时读时复制可以同时连带 -wal / -shm，得到一致的快照。
func copyToTempReadonly(src string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "cursor-state-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	for _, suffix := range []string{"", "-wal", "-shm"} {
		from := src + suffix
		fi, err := os.Stat(from)
		if err != nil || fi.IsDir() {
			continue
		}
		to := filepath.Join(tempDir, "state.vscdb"+suffix)
		if err := copyFile(from, to); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("拷贝 %s: %w", filepath.Base(from), err)
		}
	}
	return filepath.Join(tempDir, "state.vscdb"), cleanup, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// LoadCursorSession 读 state.vscdb，抽出当前登录的 access token / 邮箱。
// 任何环节失败都返回 nil + 错误，调用方应静默降级——这不是关键路径。
func LoadCursorSession(ctx context.Context) (*CursorSession, error) {
	path, err := resolveCursorStateDBPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("Cursor state.vscdb 不存在: %w", err)
	}

	copyPath, cleanup, err := copyToTempReadonly(path)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	dsn := "file:" + filepath.ToSlash(copyPath) + "?mode=ro&_pragma=busy_timeout(2000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 state.vscdb: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	values, err := readItemTable(dbCtx, db, []string{
		"cursorAuth/accessToken",
		"cursorAuth/cachedEmail",
		"cursorAuth/stripeMembershipType",
	})
	if err != nil {
		return nil, err
	}

	token := strings.TrimSpace(values["cursorAuth/accessToken"])
	if token == "" {
		return nil, fmt.Errorf("Cursor 未登录（state.vscdb 中无 accessToken）")
	}
	sub, _ := jwtSub(token)
	return &CursorSession{
		AccessToken: token,
		UserID:      sub,
		Email:       strings.TrimSpace(values["cursorAuth/cachedEmail"]),
		Membership:  strings.TrimSpace(values["cursorAuth/stripeMembershipType"]),
	}, nil
}

func readItemTable(ctx context.Context, db *sql.DB, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		var v sql.NullString
		err := db.QueryRowContext(ctx, "SELECT value FROM ItemTable WHERE key = ?", k).Scan(&v)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, fmt.Errorf("查询 %s: %w", k, err)
		}
		if v.Valid {
			out[k] = v.String
		}
	}
	return out, nil
}

// jwtSub 不做签名校验，仅从 payload 提取 sub。
// 这只是为了拼 Cursor API 的 user 参数；权威鉴权由服务端做。
func jwtSub(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("非 JWT 形态")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// 兼容带 padding 的 base64
		payload, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return "", err
		}
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	// Cursor 的 sub 形如 "auth0|user_01..."，API 实际只用 "user_01..." 部分
	if i := strings.LastIndex(claims.Sub, "|"); i >= 0 {
		return claims.Sub[i+1:], nil
	}
	return claims.Sub, nil
}
