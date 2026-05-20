package main

import (
	"encoding/json"
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
