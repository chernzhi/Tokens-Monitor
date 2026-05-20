package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
		{"3.3.0", "garbage", 1},
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
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
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

func TestUpdaterCheckNow_HitsServerAndReturnsInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/release/client/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("current") != "3.2.9" {
			t.Errorf("missing current param: %q", r.URL.Query().Get("current"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latest_version":"3.3.0","has_update":true,"download_url":"/api/release/client/download/ai-monitor-3.3.0.exe","sha256":"deadbeef","size_bytes":7,"release_notes":""}`))
	}))
	defer srv.Close()

	u := &Updater{serverURL: srv.URL, currentVersion: "3.2.9", checkClient: &http.Client{}, downloadClient: &http.Client{}, cfg: &Config{}}
	info, err := u.CheckNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasUpdate || info.LatestVersion != "3.3.0" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestUpdaterCheckNow_404TreatedAsNoRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	u := &Updater{serverURL: srv.URL, currentVersion: "3.2.9", checkClient: &http.Client{}, downloadClient: &http.Client{}, cfg: &Config{}}
	info, err := u.CheckNow(context.Background())
	if err != nil || info != nil {
		t.Errorf("404 should return (nil,nil); got info=%v err=%v", info, err)
	}
}

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

func TestUpdaterDownload_VerifiesSha256(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)

	payload := []byte("payload-bytes")
	wantSum := sha256.Sum256(payload)
	want := hex.EncodeToString(wantSum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	u := &Updater{serverURL: srv.URL, currentVersion: "3.2.9", checkClient: &http.Client{}, downloadClient: &http.Client{}, cfg: &Config{}}
	info := &ReleaseInfo{DownloadURL: "/", SHA256: want, SizeBytes: int64(len(payload)), LatestVersion: "3.3.0-test"}
	path, err := u.downloadToTemp(info)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("download file missing: %v", err)
	}
	// 篡改 sha 应失败
	bad := *info
	bad.SHA256 = "00"
	bad.LatestVersion = "3.3.0-bad"
	if _, err := u.downloadToTemp(&bad); err == nil {
		t.Errorf("expected sha mismatch error")
	}
}
