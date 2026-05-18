//go:build !windows

package main

import "errors"

// openWizardWindow is a Windows-only feature. On other platforms callers
// fall back to openBrowser.
func openWizardWindow(url, title string, closeOnRequest *func()) (<-chan struct{}, error) {
	return nil, errors.New("embedded wizard window is only supported on Windows")
}
