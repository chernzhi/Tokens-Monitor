//go:build !windows

package main

import "fmt"

func doForceCleanup() {
	fmt.Println("--force-cleanup 仅 Windows 实现")
}
