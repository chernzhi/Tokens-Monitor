package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/mod/semver"
)

const (
	updaterTempDirName = "ai-monitor-update"
	defaultCheckEvery  = 1 * time.Hour
	firstCheckDelay    = 10 * time.Second
	downloadTimeout    = 5 * time.Minute
	updaterPlatform    = "win32-x64"
)

// ReleaseInfo 对应后端 /api/release/client/latest 响应。
type ReleaseInfo struct {
	LatestVersion  string `json:"latest_version"`
	CurrentVersion string `json:"current_version"`
	HasUpdate      bool   `json:"has_update"`
	DownloadURL    string `json:"download_url"`
	SHA256         string `json:"sha256"`
	SizeBytes      int64  `json:"size_bytes"`
	ReleaseNotes   string `json:"release_notes"`
	Mandatory      bool   `json:"mandatory"`
	PublishedAt    string `json:"published_at"`
}

// Updater polls the release server and downloads new client binaries.
type Updater struct {
	cfg            *Config
	serverURL      string
	currentVersion string
	checkClient    *http.Client
	downloadClient *http.Client

	mu             sync.RWMutex
	latest         *ReleaseInfo
	lastError      string
	downloadedPath string

	downloading atomic.Bool
	progressPct atomic.Int32 // 0..100
}

func NewUpdater(cfg *Config) *Updater {
	srv := strings.TrimRight(strings.TrimSpace(cfg.UpdateCheckURL), "/")
	if srv == "" {
		srv = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	}
	return &Updater{
		cfg:            cfg,
		serverURL:      srv,
		currentVersion: Version,
		checkClient:    &http.Client{Timeout: 30 * time.Second},
		downloadClient: &http.Client{}, // 下载依赖 context 控时长，避免 30s 截流大文件
	}
}

func (u *Updater) Start(ctx context.Context) {
	interval := time.Duration(u.cfg.UpdateCheckIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = defaultCheckEvery
	}
	timer := time.NewTimer(firstCheckDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if _, err := u.CheckNow(ctx); err != nil {
				log.Printf("[updater] 检查失败: %v", err)
			}
			timer.Reset(interval)
		}
	}
}

func (u *Updater) CheckNow(ctx context.Context) (*ReleaseInfo, error) {
	if u.serverURL == "" {
		return nil, errors.New("updater: 未配置上报服务地址")
	}
	q := url.Values{}
	q.Set("current", u.currentVersion)
	q.Set("platform", updaterPlatform)
	endpoint := u.serverURL + "/api/release/client/latest?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.checkClient.Do(req)
	if err != nil {
		u.setError(err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		u.mu.Lock()
		u.lastError = ""
		u.mu.Unlock()
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("HTTP %d", resp.StatusCode)
		u.setError(err.Error())
		return nil, err
	}
	var info ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		u.setError(err.Error())
		return nil, err
	}
	if info.HasUpdate {
		log.Printf("[updater] 检测到新版本 v%s", info.LatestVersion)
	}
	u.setLatest(&info)
	return &info, nil
}

func (u *Updater) setLatest(info *ReleaseInfo) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.latest = info
	u.lastError = ""
}

func (u *Updater) setError(msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lastError = msg
}

func (u *Updater) Snapshot() (*ReleaseInfo, string, int32, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.latest, u.lastError, u.progressPct.Load(), u.downloading.Load()
}

// downloadToTemp 把 info.DownloadURL 流式下载到 %TEMP%\ai-monitor-update\，
// 校验 sha256 / size_bytes，成功后原子改名为最终文件并返回完整路径。
func (u *Updater) downloadToTemp(info *ReleaseInfo) (string, error) {
	if info == nil || info.DownloadURL == "" || info.SHA256 == "" {
		return "", errors.New("updater: ReleaseInfo 缺 download_url 或 sha256")
	}
	if !u.downloading.CompareAndSwap(false, true) {
		return "", errors.New("download already in progress")
	}
	defer u.downloading.Store(false)

	tmpDir := filepath.Join(os.TempDir(), updaterTempDirName)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", err
	}
	finalName := fmt.Sprintf("ai-monitor-%s.exe", info.LatestVersion)
	partPath := filepath.Join(tmpDir, finalName+".part")
	finalPath := filepath.Join(tmpDir, finalName)

	u.progressPct.Store(0)

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	urlStr := info.DownloadURL
	if strings.HasPrefix(urlStr, "/") {
		urlStr = u.serverURL + urlStr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", err
	}
	resp, err := u.downloadClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(partPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	removePart := func() {
		if rerr := os.Remove(partPath); rerr != nil && !os.IsNotExist(rerr) {
			log.Printf("[updater] 清理临时文件失败 %s: %v", partPath, rerr)
		}
	}
	h := sha256.New()
	var written int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				removePart()
				return "", werr
			}
			h.Write(buf[:n])
			written += int64(n)
			if info.SizeBytes > 0 {
				u.progressPct.Store(int32(written * 100 / info.SizeBytes))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			removePart()
			return "", rerr
		}
	}
	if err := f.Sync(); err != nil {
		removePart()
		return "", err
	}
	if err := f.Close(); err != nil {
		removePart()
		return "", err
	}

	if info.SizeBytes > 0 && written != info.SizeBytes {
		removePart()
		return "", fmt.Errorf("大小不符: got %d want %d", written, info.SizeBytes)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, info.SHA256) {
		removePart()
		return "", fmt.Errorf("sha256 不符: got %s want %s", got, info.SHA256)
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		removePart()
		return "", err
	}
	u.progressPct.Store(100)
	u.mu.Lock()
	u.downloadedPath = finalPath
	u.mu.Unlock()
	return finalPath, nil
}

// compareVersions returns -1 / 0 / 1 like strings.Compare.
// 非法 semver 视为最旧。
func compareVersions(a, b string) int {
	na := normalizeSemver(a)
	nb := normalizeSemver(b)
	if !semver.IsValid(na) && !semver.IsValid(nb) {
		return 0
	}
	if !semver.IsValid(na) {
		return -1
	}
	if !semver.IsValid(nb) {
		return 1
	}
	return semver.Compare(na, nb)
}

func normalizeSemver(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
