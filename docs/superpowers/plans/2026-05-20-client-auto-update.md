# 客户端自动更新 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 ai-monitor 客户端能定时检测 135 服务器上的新版本，用户在 webwizard 控制台一键完成下载→sha256 校验→替换 exe→重启的全流程。

**Architecture:** 后端新增 `/api/release/client/*` 端点扫描 `EXTENSION_DIR/client/` 下的 `ai-monitor-X.Y.Z.exe` + sidecar 哈希；客户端新增 `updater.go` 后台 1 小时轮询 + on-demand 检查；触发更新时下载到 `%TEMP%`，写一份 `updater.bat` 接力替换文件并拉起新进程。Webwizard 运行态控制台加 3 个 API + 横幅 + 关于卡片。

**Tech Stack:** Python FastAPI（后端）、Go 1.25 + `golang.org/x/mod/semver`（客户端）、Windows 批处理（接力替换）、原生 HTML/JS（UI）。

**对应设计文档：** `docs/superpowers/specs/2026-05-20-client-auto-update-design.md`

---

## File Structure

**Backend**
- Create: `backend/app/routers/release.py` — 扫描 `EXTENSION_DIR/client/`，提供 `/latest` 与 `/download`。
- Modify: `backend/app/main.py` — 注册新 router。
- Create: `backend/tests/test_release_client.py` — 单测。

**Client (Go)**
- Create: `client/updater.go` — `Updater` 主结构、轮询、check、download、sha256、ReleaseInfo JSON。
- Create: `client/updater_apply_windows.go` — Windows 下 `ApplyUpdate`、`updater.bat` 生成、`--post-update` 钩子。
- Create: `client/updater_apply_other.go` — 非 Windows 占位（直接返回 `errors.New("仅 Windows 支持自动更新")`）。
- Create: `client/updater_test.go` — semver/sha256/JSON 解析/HTTP check 单测。
- Modify: `client/config.go` — 新增字段 `UpdateCheckURL` / `UpdateCheckIntervalSeconds` / `UpdateAutoApply`。
- Modify: `client/main.go` 或入口（看现有结构定位）— 启动 Updater；处理 `--post-update` flag。
- Modify: `client/webwizard.go` — 在运行态 `serveWizard` 加 3 个端点 + 横幅与「关于」卡片 HTML。
- Modify: `client/VERSION` — 升到 `3.3.0`。
- Modify: `client/go.mod` / `go.sum` — 直接依赖 `golang.org/x/mod`。

---

## Task 1: 后端 release router 骨架

**Files:**
- Create: `backend/app/routers/release.py`
- Create: `backend/tests/test_release_client.py`
- Modify: `backend/app/main.py`

- [ ] **Step 1: 写第一个失败测试 — 空目录返回 404**

`backend/tests/test_release_client.py`：

```python
import os
import tempfile
import unittest
from pathlib import Path

from fastapi.testclient import TestClient


def _make_client(tmp: Path):
    os.environ["EXTENSION_DIR"] = str(tmp)
    # 强制重新读取 settings
    from importlib import reload
    from app import config as cfg
    reload(cfg)
    from app.routers import release
    reload(release)
    from app import main
    reload(main)
    return TestClient(main.app)


class ReleaseClientTests(unittest.TestCase):
    def test_latest_returns_404_when_no_client_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "client").mkdir()
            client = _make_client(Path(tmp))
            resp = client.get("/api/release/client/latest?current=3.2.9&platform=win32-x64")
            self.assertEqual(resp.status_code, 404)


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `cd backend && python -m pytest tests/test_release_client.py -v`
Expected: `ImportError: cannot import name 'release'` 或 404 路由不存在。

- [ ] **Step 3: 创建最小 release.py 让 404 通过**

`backend/app/routers/release.py`：

```python
"""Client binary release distribution.

Scans EXTENSION_DIR/client/ for files matching ai-monitor-X.Y.Z.exe,
returns latest semver as JSON, and serves the binary + sha256 sidecar.
"""

import re
from pathlib import Path

from fastapi import APIRouter, HTTPException
from fastapi.responses import FileResponse

from app.config import settings

router = APIRouter(prefix="/api/release", tags=["release"])

EXTENSION_DIR = Path(getattr(settings, "EXTENSION_DIR", "/opt/token-monitor/extensions"))
CLIENT_DIR = EXTENSION_DIR / "client"

_EXE_RE = re.compile(r"^ai-monitor-(?P<version>\d+\.\d+\.\d+)\.exe$")


def _parse_semver(v: str) -> tuple[int, ...]:
    return tuple(int(x) for x in v.split("."))


def _scan_latest_client() -> dict | None:
    if not CLIENT_DIR.is_dir():
        return None
    best = None
    for f in CLIENT_DIR.iterdir():
        m = _EXE_RE.match(f.name)
        if not m:
            continue
        version = m.group("version")
        sha_file = CLIENT_DIR / (f.name + ".sha256")
        if not sha_file.is_file():
            continue
        if best is None or _parse_semver(version) > _parse_semver(best["version"]):
            sha = sha_file.read_text(encoding="utf-8").strip().split()[0]
            notes_file = CLIENT_DIR / f"ai-monitor-{version}.md"
            notes = notes_file.read_text(encoding="utf-8") if notes_file.is_file() else ""
            best = {
                "version": version,
                "filename": f.name,
                "sha256": sha,
                "size_bytes": f.stat().st_size,
                "release_notes": notes,
            }
    return best


@router.get("/client/latest")
async def latest_client(current: str = "", platform: str = "win32-x64"):
    if platform != "win32-x64":
        raise HTTPException(404, f"platform {platform} not supported")
    info = _scan_latest_client()
    if info is None:
        raise HTTPException(404, "no client release available")
    has_update = False
    if current:
        try:
            has_update = _parse_semver(info["version"]) > _parse_semver(current)
        except ValueError:
            has_update = True
    return {
        "latest_version": info["version"],
        "current_version": current,
        "has_update": has_update,
        "download_url": f"/api/release/client/download/{info['filename']}",
        "sha256": info["sha256"],
        "size_bytes": info["size_bytes"],
        "release_notes": info["release_notes"],
        "mandatory": False,
        "published_at": "",
    }


@router.get("/client/download/{filename}")
async def download_client(filename: str):
    if not _EXE_RE.match(filename):
        raise HTTPException(400, "invalid filename")
    fp = CLIENT_DIR / filename
    if not fp.is_file():
        raise HTTPException(404, "file not found")
    sha_file = CLIENT_DIR / (filename + ".sha256")
    headers = {}
    if sha_file.is_file():
        headers["ETag"] = sha_file.read_text(encoding="utf-8").strip().split()[0]
    return FileResponse(fp, media_type="application/octet-stream", filename=filename, headers=headers)
```

- [ ] **Step 4: 在 main.py 注册路由**

修改 `backend/app/main.py` 第 8 行附近：

```python
from app.routers import collect, dashboard, extension, release, user_auth
```

并在 `include_router` 段（约 49 行后）追加：

```python
app.include_router(release.router)
```

- [ ] **Step 5: 再跑测试，确认 404 通过**

Run: `cd backend && python -m pytest tests/test_release_client.py -v`
Expected: PASS。

- [ ] **Step 6: 补充更多用例**

继续追加到 `test_release_client.py`：

```python
    def test_latest_returns_release_info_when_files_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            cdir = Path(tmp) / "client"
            cdir.mkdir()
            (cdir / "ai-monitor-3.3.0.exe").write_bytes(b"fake-exe")
            (cdir / "ai-monitor-3.3.0.exe.sha256").write_text(
                "abcd1234" * 8 + "\n", encoding="utf-8"
            )
            (cdir / "ai-monitor-3.3.0.md").write_text("release notes", encoding="utf-8")
            (cdir / "ai-monitor-3.2.9.exe").write_bytes(b"old")
            (cdir / "ai-monitor-3.2.9.exe.sha256").write_text("00" * 32, encoding="utf-8")

            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/latest?current=3.2.9")
            self.assertEqual(r.status_code, 200)
            body = r.json()
            self.assertEqual(body["latest_version"], "3.3.0")
            self.assertTrue(body["has_update"])
            self.assertEqual(body["size_bytes"], len(b"fake-exe"))
            self.assertIn("3.3.0", body["download_url"])

    def test_latest_no_update_when_current_already_latest(self):
        with tempfile.TemporaryDirectory() as tmp:
            cdir = Path(tmp) / "client"
            cdir.mkdir()
            (cdir / "ai-monitor-3.3.0.exe").write_bytes(b"x")
            (cdir / "ai-monitor-3.3.0.exe.sha256").write_text("00" * 32, encoding="utf-8")
            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/latest?current=3.3.0")
            self.assertEqual(r.status_code, 200)
            self.assertFalse(r.json()["has_update"])

    def test_download_rejects_path_traversal(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "client").mkdir()
            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/download/..%2Fpasswd")
            self.assertIn(r.status_code, (400, 404))

    def test_download_404_for_missing_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "client").mkdir()
            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/download/ai-monitor-9.9.9.exe")
            self.assertEqual(r.status_code, 404)

    def test_download_returns_file_with_etag(self):
        with tempfile.TemporaryDirectory() as tmp:
            cdir = Path(tmp) / "client"
            cdir.mkdir()
            (cdir / "ai-monitor-3.3.0.exe").write_bytes(b"payload")
            (cdir / "ai-monitor-3.3.0.exe.sha256").write_text("deadbeef" * 8, encoding="utf-8")
            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/download/ai-monitor-3.3.0.exe")
            self.assertEqual(r.status_code, 200)
            self.assertEqual(r.content, b"payload")
            self.assertEqual(r.headers.get("etag"), "deadbeef" * 8)
```

- [ ] **Step 7: 跑全套测试，确认全部通过**

Run: `cd backend && python -m pytest tests/test_release_client.py -v`
Expected: 5 passed.

- [ ] **Step 8: 提交**

```bash
git add backend/app/routers/release.py backend/app/main.py backend/tests/test_release_client.py
git commit -m "feat(backend): /api/release/client/* 端点扫描客户端版本"
```

---

## Task 2: 客户端 — Updater 核心类型与 semver/sha256 工具

**Files:**
- Create: `client/updater.go`
- Create: `client/updater_test.go`
- Modify: `client/go.mod`, `client/go.sum`

- [ ] **Step 1: 加直接依赖**

Run:
```bash
cd client && go get golang.org/x/mod/semver@latest
```

- [ ] **Step 2: 写失败的 semver 比较测试**

`client/updater_test.go`：

```go
package main

import (
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"3.2.9", "3.3.0", -1},
		{"3.3.0", "3.2.9", 1},
		{"3.3.0", "3.3.0", 0},
		{"v3.3.0", "3.3.0", 0},
		{"3.3.0", "v3.3.0", 0},
		{"3.3.0", "garbage", 1}, // garbage 当作最旧
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if (got < 0 && c.want >= 0) || (got > 0 && c.want <= 0) || (got == 0 && c.want != 0) {
			t.Errorf("compareVersions(%q,%q)=%d want sign %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSha256File(t *testing.T) {
	tmp := t.TempDir()
	p := tmp + "/x.bin"
	if err := writeFile(p, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := sha256File(p)
	if err != nil {
		t.Fatal(err)
	}
	// echo -n hello | sha256sum
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if !strings.EqualFold(got, want) {
		t.Errorf("sha256File=%s want %s", got, want)
	}
}

func writeFile(p string, b []byte) error {
	return osWriteFile(p, b)
}
```

(注：`osWriteFile` 包装 `os.WriteFile`，避免 test 直接依赖 os；可省略，下一步直接用 `os.WriteFile`)。

- [ ] **Step 3: 跑测试，确认编译失败**

Run: `cd client && go test ./... -run TestCompareVersions -v`
Expected: `undefined: compareVersions` / `undefined: sha256File`.

- [ ] **Step 4: 创建 updater.go 最小实现**

`client/updater.go`：

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"golang.org/x/mod/semver"
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
```

更新 test 中 `osWriteFile` 用 `os.WriteFile`：

```go
import "os"
// ...
func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }
```

- [ ] **Step 5: 跑测试，确认通过**

Run: `cd client && go test -run "TestCompareVersions|TestSha256File" -v`
Expected: PASS。

- [ ] **Step 6: 加 ReleaseInfo JSON 解析测试**

追加到 `updater_test.go`：

```go
func TestReleaseInfoUnmarshal(t *testing.T) {
	body := []byte(`{"latest_version":"3.3.0","current_version":"3.2.9","has_update":true,"download_url":"/api/release/client/download/ai-monitor-3.3.0.exe","sha256":"abc","size_bytes":18874368,"release_notes":"x","mandatory":false,"published_at":""}`)
	var info ReleaseInfo
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatal(err)
	}
	if info.LatestVersion != "3.3.0" || !info.HasUpdate || info.SizeBytes != 18874368 {
		t.Fatalf("bad parse: %+v", info)
	}
}
```

并 `import "encoding/json"`。Run: `go test -run TestReleaseInfoUnmarshal -v`. Expected: PASS.

- [ ] **Step 7: 提交**

```bash
git add client/updater.go client/updater_test.go client/go.mod client/go.sum
git commit -m "feat(client): updater 基础类型与 semver/sha256 工具"
```

---

## Task 3: 客户端 — Updater check + download

**Files:**
- Modify: `client/updater.go`
- Modify: `client/updater_test.go`

- [ ] **Step 1: 写失败测试 — check 命中**

追加到 `updater_test.go`：

```go
import (
	"net/http"
	"net/http/httptest"
)

func TestUpdaterCheckNow_HitsServerAndReturnsInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/release/client/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("current") != "3.2.9" {
			t.Errorf("missing current param")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"latest_version":"3.3.0","has_update":true,"download_url":"/api/release/client/download/ai-monitor-3.3.0.exe","sha256":"deadbeef","size_bytes":7,"release_notes":""}`))
	}))
	defer srv.Close()

	u := &Updater{serverURL: srv.URL, currentVersion: "3.2.9", client: &http.Client{}}
	info, err := u.CheckNow()
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasUpdate || info.LatestVersion != "3.3.0" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestUpdaterDownload_VerifiesSha256(t *testing.T) {
	payload := []byte("payload-bytes")
	// sha256("payload-bytes")
	want := "01ab1f3e16a8b9f01b2c5f8df5d33d6daa3a6ba85d18a26b6f538e02fafff7e1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()
	u := &Updater{serverURL: srv.URL, currentVersion: "3.2.9", client: &http.Client{}}
	info := &ReleaseInfo{DownloadURL: "/", SHA256: want, SizeBytes: int64(len(payload)), LatestVersion: "3.3.0"}
	path, err := u.downloadToTemp(info)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("download file missing: %v", err)
	}
	// 篡改 sha 应失败
	info.SHA256 = "00"
	if _, err := u.downloadToTemp(info); err == nil {
		t.Errorf("expected sha mismatch error")
	}
}
```

(注意：测试里的 sha256 hex 是示意；执行时替换为真实值，可临时用 `t.Logf` 打印，或本地计算后填入。)

- [ ] **Step 2: 跑测试确认失败**

Run: `cd client && go test -run "TestUpdaterCheckNow_|TestUpdaterDownload_" -v`
Expected: `undefined: Updater`。

- [ ] **Step 3: 扩 updater.go — Updater 结构 + CheckNow + download**

追加到 `client/updater.go`：

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	updaterTempDirName = "ai-monitor-update"
	defaultCheckEvery  = 1 * time.Hour
	firstCheckDelay    = 10 * time.Second
	downloadTimeout    = 5 * time.Minute
)

type Updater struct {
	cfg            *Config
	serverURL      string
	currentVersion string
	client         *http.Client

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
		client:         &http.Client{Timeout: 30 * time.Second},
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
			if _, err := u.CheckNow(); err != nil {
				log.Printf("[updater] 检查失败: %v", err)
			}
			timer.Reset(interval)
		}
	}
}

func (u *Updater) CheckNow() (*ReleaseInfo, error) {
	if u.serverURL == "" {
		return nil, errors.New("updater: 未配置上报服务地址")
	}
	q := url.Values{}
	q.Set("current", u.currentVersion)
	q.Set("platform", "win32-x64")
	endpoint := u.serverURL + "/api/release/client/latest?" + q.Encode()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		u.setError(err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		u.setLatest(nil)
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

// downloadToTemp 把 info.DownloadURL 流式下载到 %TEMP%\ai-monitor-update\，并校验 sha256/size。
func (u *Updater) downloadToTemp(info *ReleaseInfo) (string, error) {
	if info == nil || info.DownloadURL == "" || info.SHA256 == "" {
		return "", errors.New("updater: ReleaseInfo 缺 download_url 或 sha256")
	}
	tmpDir := filepath.Join(os.TempDir(), updaterTempDirName)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", err
	}
	finalName := fmt.Sprintf("ai-monitor-%s.exe", info.LatestVersion)
	partPath := filepath.Join(tmpDir, finalName+".part")
	finalPath := filepath.Join(tmpDir, finalName)

	u.downloading.Store(true)
	u.progressPct.Store(0)
	defer u.downloading.Store(false)

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
	resp, err := u.client.Do(req)
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
	h := sha256.New()
	var written int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(partPath)
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
			f.Close()
			os.Remove(partPath)
			return "", rerr
		}
	}
	f.Close()

	if info.SizeBytes > 0 && written != info.SizeBytes {
		os.Remove(partPath)
		return "", fmt.Errorf("大小不符: got %d want %d", written, info.SizeBytes)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, info.SHA256) {
		os.Remove(partPath)
		return "", fmt.Errorf("sha256 不符: got %s want %s", got, info.SHA256)
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		return "", err
	}
	u.progressPct.Store(100)
	u.mu.Lock()
	u.downloadedPath = finalPath
	u.mu.Unlock()
	return finalPath, nil
}
```

(注意：`Version` 常量已在 client 包中存在；`Config` 字段在 Task 5 加。)

- [ ] **Step 4: 编译失败 — `cfg.UpdateCheckURL` 等字段未定义**

为保持编译通过，**临时**在 `updater.go` 顶部加一个本地结构（之后 Task 5 删掉，统一改用 `Config`）：

```go
// 兼容编译：实际字段在 Task 5 合入 Config 后删除。
```

或者直接在 `Config` 上一次性加好（推荐先做 Task 5 的字段合入再回来）。为顺序起见，**先跳到 Task 5** 加字段，再回来跑测试。

- [ ] **Step 5: 完成 Config 字段后回来跑测试**

填入正确的 sha256（用 `python -c "import hashlib;print(hashlib.sha256(b'payload-bytes').hexdigest())"` 计算），更新 test。
Run: `cd client && go test -run "TestUpdater" -v`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add client/updater.go client/updater_test.go
git commit -m "feat(client): Updater check + sha256 校验下载"
```

---

## Task 4: Config 字段 + 入口启动 Updater

**Files:**
- Modify: `client/config.go`
- Modify: `client/main.go`（或现有的入口文件；以 `grep -l "func main" client/*.go` 实际定位）

- [ ] **Step 1: 在 Config 结构里加三字段**

打开 `client/config.go`，在 Config struct 内部追加：

```go
	// 自动更新相关（空 / 0 视为默认）
	UpdateCheckURL             string `json:"update_check_url,omitempty"`
	UpdateCheckIntervalSeconds int    `json:"update_check_interval_seconds,omitempty"`
	UpdateAutoApply            bool   `json:"update_auto_apply,omitempty"`
```

- [ ] **Step 2: 在 ProxyServer 或 main 入口里启动 Updater**

`grep -n "func main\|StartProxy\|go reporter.Start" client/*.go`，在与 `reporter.Start(ctx)` 同一段（很可能在 `main.go` 或 `proxy.go`）下方追加：

```go
	updater := NewUpdater(cfg)
	go updater.Start(ctx)
```

并把 `updater` 暴露到 webwizard 能拿到的位置（如 ProxyServer 结构加 `Updater *Updater` 字段，构造时塞进去）。

- [ ] **Step 3: 编译**

Run: `cd client && go build ./...`
Expected: 成功（无报错）。

- [ ] **Step 4: 跑 Task 3 的测试**

Run: `cd client && go test -run "TestUpdater" -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add client/config.go client/main.go
git commit -m "feat(client): Config 加自动更新字段并启动 Updater 后台轮询"
```

---

## Task 5: Windows 替换 + 重启（updater.bat + post-update 钩子）

**Files:**
- Create: `client/updater_apply_windows.go`
- Create: `client/updater_apply_other.go`
- Modify: `client/updater.go`（暴露 `ApplyUpdate`）
- Modify: `client/main.go`（处理 `--post-update` flag）
- Modify: `client/updater_test.go`（bat 内容快照测试）

- [ ] **Step 1: 写失败测试 — bat 内容快照**

`updater_test.go` 追加：

```go
func TestRenderUpdaterBat_ContainsKeyTokens(t *testing.T) {
	got := renderUpdaterBat()
	for _, s := range []string{
		"setlocal",
		"copy /Y",
		"tasklist /FI",
		"move /Y",
		"start \"\"",
		"--post-update",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("bat missing %q", s)
		}
	}
}
```

Run: `cd client && go test -run TestRenderUpdaterBat -v`
Expected: `undefined: renderUpdaterBat`。

- [ ] **Step 2: 创建 updater_apply_windows.go**

```go
//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const updaterBatTemplate = `@echo off
setlocal
set TARGET=%~1
set NEW=%~2
set BACKUP=%~3
set LOG=%~4
set PARENTPID=%~5

REM 备份当前 exe
copy /Y "%TARGET%" "%BACKUP%" >>"%LOG%" 2>&1

REM 等待父进程退出（最多 30s）
set /a TRIES=0
:waitloop
tasklist /FI "PID eq %PARENTPID%" 2>nul | find "%PARENTPID%" >nul
if errorlevel 1 goto replace
set /a TRIES+=1
if %TRIES% GEQ 60 goto fail
ping -n 2 127.0.0.1 >nul
goto waitloop

:replace
move /Y "%NEW%" "%TARGET%" >>"%LOG%" 2>&1
if errorlevel 1 goto rollback

start "" "%TARGET%" --post-update "%BACKUP%"
exit /b 0

:rollback
copy /Y "%BACKUP%" "%TARGET%" >>"%LOG%" 2>&1
start "" "%TARGET%"
exit /b 1

:fail
echo updater: parent did not exit within 30s >>"%LOG%"
exit /b 2
`

func renderUpdaterBat() string { return updaterBatTemplate }

// ApplyUpdate 写 bat 并 detach 启动，自身退出。
func (u *Updater) ApplyUpdate(info *ReleaseInfo) error {
	if info == nil {
		return fmt.Errorf("无可用更新")
	}
	newExe, err := u.downloadToTemp(info)
	if err != nil {
		return err
	}
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	currentExe, _ = filepath.Abs(currentExe)

	tmpDir := filepath.Dir(newExe)
	batPath := filepath.Join(tmpDir, "updater.bat")
	logPath := filepath.Join(tmpDir, "updater.log")
	backupPath := filepath.Join(tmpDir, fmt.Sprintf("ai-monitor-backup-%d.exe", time.Now().Unix()))

	if err := os.WriteFile(batPath, []byte(renderUpdaterBat()), 0o755); err != nil {
		return err
	}

	cmd := newDetachedCmd("cmd", "/c", batPath,
		currentExe, newExe, backupPath, logPath, fmt.Sprint(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("[updater] 已派发 updater.bat (pid=%d)，本进程将退出以释放 exe", cmd.Process.Pid)
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// PostUpdateCleanup 在 --post-update <backup> 启动时调用：
// 30 秒后若本进程仍存活，删除备份文件。
func PostUpdateCleanup(backupPath string) {
	log.Printf("[updater] ✅ 已更新到 v%s（备份: %s）", Version, backupPath)
	go func() {
		time.Sleep(30 * time.Second)
		if backupPath == "" {
			return
		}
		if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[updater] 备份清理失败: %v", err)
			return
		}
		log.Printf("[updater] 备份已清理")
	}()
}

// 保持 exec 包导入（用于未来扩展，例如 cmd.Wait 在调试场景）。
var _ = exec.Command
```

- [ ] **Step 3: 非 Windows stub**

`client/updater_apply_other.go`：

```go
//go:build !windows

package main

import (
	"errors"
)

func renderUpdaterBat() string { return "" }

func (u *Updater) ApplyUpdate(info *ReleaseInfo) error {
	return errors.New("当前平台暂不支持一键更新（仅 Windows）")
}

func PostUpdateCleanup(backupPath string) {}
```

- [ ] **Step 4: 在 main.go 处理 --post-update flag**

`grep -n "flag.Parse\|flag.StringVar" client/main.go`，在 flag 段加：

```go
postUpdate := flag.String("post-update", "", "（内部）由 updater.bat 调用，传入备份文件路径用于成功后清理")
```

并在 `flag.Parse()` 之后、proxy 启动之前：

```go
if *postUpdate != "" {
	PostUpdateCleanup(*postUpdate)
}
```

- [ ] **Step 5: 跑 bat 测试**

Run: `cd client && go test -run TestRenderUpdaterBat -v`
Expected: PASS。

- [ ] **Step 6: 编译全套**

Run: `cd client && go build ./... && go test ./...`
Expected: 全部通过。

- [ ] **Step 7: 提交**

```bash
git add client/updater_apply_windows.go client/updater_apply_other.go \
        client/updater.go client/updater_test.go client/main.go
git commit -m "feat(client): updater.bat 接力替换 exe + --post-update 钩子"
```

---

## Task 6: Webwizard 运行态 UI — 横幅 + 关于卡片 + 3 个 API

**Files:**
- Modify: `client/webwizard.go`
- Modify: `client/webwizard_render_test.go`（如果存在）

- [ ] **Step 1: 在运行态 `serveWizard` 加 3 个端点**

在 `client/webwizard.go` 的 `serveWizard` 函数（≈1343 行）里，与其它 `subPath` 分支并列追加：

```go
	if subPath == "/api/wizard/update/status" && r.Method == http.MethodGet {
		info, lastErr, pct, downloading := s.Updater.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"current_version": Version,
			"release":         info,
			"error":           lastErr,
			"progress":        pct,
			"downloading":     downloading,
		})
		return
	}
	if subPath == "/api/wizard/update/check" && r.Method == http.MethodPost {
		if !s.authorizeWizardAction(r) {
			s.rejectUnauthorizedWizardAction(w)
			return
		}
		info, err := s.Updater.CheckNow()
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"release": info})
		return
	}
	if subPath == "/api/wizard/update/apply" && r.Method == http.MethodPost {
		if !s.authorizeWizardAction(r) {
			s.rejectUnauthorizedWizardAction(w)
			return
		}
		info, _, _, _ := s.Updater.Snapshot()
		if info == nil || !info.HasUpdate {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "当前没有可用的新版本"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "starting"})
		go func() {
			if err := s.Updater.ApplyUpdate(info); err != nil {
				log.Printf("[updater] ApplyUpdate 失败: %v", err)
			}
		}()
		return
	}
```

- [ ] **Step 2: 在控制台 HTML 模板里加横幅 + 关于卡片**

`webwizard.go` 顶部某处（搜 `webConsoleHTML` 或 `const consoleHTML`）找到运行态控制台模板，在合适位置插入：

```html
<!-- 自动更新横幅（无新版时隐藏） -->
<div id="updateBanner" style="display:none;padding:12px 16px;border-radius:8px;margin:12px 0;"></div>

<!-- 关于卡片 -->
<div class="card" id="aboutCard" style="margin-top:24px">
  <h3>关于</h3>
  <div>当前版本：v<span id="curVer"></span></div>
  <div>最新版本：<span id="latestVer">检查中…</span></div>
  <div style="margin-top:8px">
    <button onclick="checkUpdate()">检查更新</button>
    <button id="applyBtn" onclick="applyUpdate()" disabled>立即更新</button>
  </div>
  <pre id="releaseNotes" style="margin-top:12px;white-space:pre-wrap;font-size:12px;color:#666"></pre>
</div>

<script>
function renderUpdateState(s) {
  document.getElementById('curVer').textContent = s.current_version || '';
  var banner = document.getElementById('updateBanner');
  var applyBtn = document.getElementById('applyBtn');
  var latest = document.getElementById('latestVer');
  var notes = document.getElementById('releaseNotes');
  if (s.release && s.release.has_update) {
    latest.textContent = 'v' + s.release.latest_version;
    notes.textContent = s.release.release_notes || '';
    applyBtn.disabled = false;
    banner.style.display = 'block';
    banner.style.background = s.release.mandatory ? '#fee2e2' : '#fef3c7';
    banner.innerHTML = '🆕 新版本 v' + s.release.latest_version +
      ' 可用 · <button onclick="applyUpdate()">立即更新</button>';
  } else {
    latest.textContent = '已是最新';
    applyBtn.disabled = true;
    banner.style.display = 'none';
  }
  if (s.downloading) {
    banner.style.display = 'block';
    banner.innerHTML = '下载中… ' + s.progress + '%';
  }
  if (s.error) {
    banner.style.display = 'block';
    banner.style.background = '#fecaca';
    banner.textContent = '更新检查失败: ' + s.error;
  }
}
function refreshUpdateStatus() {
  fetch(basePath + '/api/wizard/update/status')
    .then(function(r){return r.json();})
    .then(renderUpdateState);
}
function checkUpdate() {
  fetch(basePath + '/api/wizard/update/check', {method:'POST', headers: wizardHeaders()})
    .then(function(r){return r.json();})
    .then(function(){ refreshUpdateStatus(); });
}
function applyUpdate() {
  if (!confirm('确认立即更新？应用会自动重启。')) return;
  fetch(basePath + '/api/wizard/update/apply', {method:'POST', headers: wizardHeaders()});
}
setInterval(refreshUpdateStatus, 5000);
refreshUpdateStatus();
</script>
```

（`wizardHeaders()` 是控制台 HTML 已有的工具，用于把 wizard token 放进 header；若名字不同，照搬其它 POST 调用的写法。）

- [ ] **Step 3: 编译并跑 render 测试**

Run: `cd client && go build ./... && go test ./... -run Render`
Expected: 通过。

- [ ] **Step 4: 手动 smoke**

```bash
cd client && go build -o ai-monitor.exe .
./ai-monitor.exe --config config.json
```

打开控制台 URL，确认「关于」卡片显示当前版本，「检查更新」按钮不报错（在 135 上尚无 release 时返回 404，UI 显示「已是最新」）。

- [ ] **Step 5: 提交**

```bash
git add client/webwizard.go
git commit -m "feat(client): 控制台加更新横幅、关于卡片与 3 个更新 API"
```

---

## Task 7: 发布 v3.2.9 基线 + 端到端验证

**Files:**
- Modify: `client/VERSION` （→ `3.3.0`）
- Modify: `client/打包-分发.bat` 或 `build.bat`（如需）

- [ ] **Step 1: 用现有 build 流程打 v3.2.9**

Run: `cd client && bash build.bat` （或 `打包-分发.bat`，按现有惯例）
确认 `dist/ai-monitor.exe` 产生。

- [ ] **Step 2: 计算 sha256 并上传到 135**

```bash
# 在本地
sha256sum dist/ai-monitor.exe | awk '{print $1}' > dist/ai-monitor-3.2.9.exe.sha256
cp dist/ai-monitor.exe dist/ai-monitor-3.2.9.exe

# 上传（示例路径，按实际服务器调整）
scp dist/ai-monitor-3.2.9.exe dist/ai-monitor-3.2.9.exe.sha256 \
    user@135:/opt/token-monitor/extensions/client/
```

- [ ] **Step 3: 在 135 上 curl 验证端点**

```bash
curl 'http://135-server/api/release/client/latest?current=3.2.8&platform=win32-x64'
```

Expected JSON：`has_update=true`, `latest_version=3.2.9`。

- [ ] **Step 4: bump 到 v3.3.0 + 重新打包**

```bash
echo "3.3.0" > client/VERSION
cd client && bash build.bat
sha256sum dist/ai-monitor.exe | awk '{print $1}' > dist/ai-monitor-3.3.0.exe.sha256
cp dist/ai-monitor.exe dist/ai-monitor-3.3.0.exe
# release notes
cat > dist/ai-monitor-3.3.0.md <<EOF
- 客户端自动更新（首个支持自更新的版本）
EOF
scp dist/ai-monitor-3.3.0.{exe,exe.sha256,md} user@135:/opt/token-monitor/extensions/client/
```

- [ ] **Step 5: 在装着 v3.2.9 的机器上验证一键更新**

打开控制台 → 应看到横幅「v3.3.0 可用」→ 点「立即更新」→ 进程退出 → 几秒后新进程起来 → 日志含 `✅ 已更新到 v3.3.0` → 30 秒后备份被清理。

- [ ] **Step 6: 提交版本号**

```bash
git add client/VERSION
git commit -m "release: v3.3.0 — 首个支持自更新的客户端版本"
```

---

## 验收清单

- [ ] `backend/tests/test_release_client.py` 全部通过。
- [ ] `client/updater_test.go` 全部通过。
- [ ] 后端 `/api/release/client/latest` 与 `/download/<file>` 在浏览器/curl 中行为符合设计 §3.2。
- [ ] 客户端控制台显示「关于」卡片 + 当前版本号；无新版时不显示横幅。
- [ ] 服务器放上 v3.3.0 后控制台横幅出现；点「立即更新」后能自动重启到新版本，备份 30 秒后清理。
- [ ] 网络断开/sha256 不符/HTTP 404 各分支日志按设计 §8 输出，且后续轮询能自愈。
