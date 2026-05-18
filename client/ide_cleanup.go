package main

import (
	"os"
	"regexp"
)

func unpatchAIMonitorIDESettings(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(data)
	proxyLine := regexp.MustCompile(`(?m)^[ \t]*"http\.proxy"\s*:\s*"([^"]+)"\s*,?\s*\n`)
	matches := proxyLine.FindStringSubmatch(content)
	if len(matches) < 2 || !isSelfProxy(matches[1]) {
		return false, nil
	}

	changed := false
	for _, key := range []string{`"http.proxy"`, `"http.proxyStrictSSL"`, `"http.proxySupport"`} {
		pattern := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `\s*:\s*("(?:[^"\\]|\\.)*"|\S+?)\s*,?\s*\n`)
		if pattern.MatchString(content) {
			content = pattern.ReplaceAllString(content, "")
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0644)
}
