package main

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed assets/icons
var wizardIconsFS embed.FS

// wizardIconsSub 是 assets/icons 的子文件系统，去掉前缀方便 ServeContent。
var wizardIconsSub fs.FS

func init() {
	sub, err := fs.Sub(wizardIconsFS, "assets/icons")
	if err == nil {
		wizardIconsSub = sub
	}
}

// serveWizardIcon 处理 /wizard/icons/<name> 请求。命中嵌入文件返回内容并设长缓存。
func serveWizardIcon(w http.ResponseWriter, r *http.Request, subPath string) bool {
	const prefix = "/icons/"
	if !strings.HasPrefix(subPath, prefix) {
		return false
	}
	name := strings.TrimPrefix(subPath, prefix)
	if name == "" || strings.ContainsAny(name, "/\\") {
		http.NotFound(w, r)
		return true
	}
	if wizardIconsSub == nil {
		http.NotFound(w, r)
		return true
	}
	data, err := fs.ReadFile(wizardIconsSub, name)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	default:
		w.Header().Set("Content-Type", http.DetectContentType(data))
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
	return true
}
