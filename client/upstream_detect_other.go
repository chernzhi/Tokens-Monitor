//go:build !windows

package main

// readCurrentSystemProxy is a no-op on non-Windows platforms.
// On macOS/Linux the system proxy is typically configured via environment variables,
// which detectUpstreamProxy() already handles.
func readCurrentSystemProxy() string {
	return ""
}

func readCurrentProxyOverride() string {
	return ""
}

func readCurrentAutoDetect() (uint32, bool) {
	return 0, false
}

func readMachinePolicyProxy() (bool, string) {
	return false, ""
}

func fetchPACBody(pacURL string) (string, error) {
	return "", nil
}

func RestoreAutoDetect(value uint32, present bool) {}
