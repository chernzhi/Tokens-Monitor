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
