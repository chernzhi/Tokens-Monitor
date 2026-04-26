//go:build !windows

package main

// InstallPowerShellProfile is a no-op on non-Windows platforms.
func InstallPowerShellProfile(proxyAddr, caCertPath string) error { return nil }

// RemovePowerShellProfile is a no-op on non-Windows platforms.
func RemovePowerShellProfile() {}
