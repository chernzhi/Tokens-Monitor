//go:build !windows

package main

// InstallPowerShellProfile is a no-op on non-Windows platforms.
func InstallPowerShellProfile(proxyAddr, caCertPath, noProxy string) error { return nil }

// RemovePowerShellProfile is a no-op on non-Windows platforms.
func RemovePowerShellProfile() {}
