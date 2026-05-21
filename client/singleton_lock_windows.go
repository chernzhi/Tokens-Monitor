//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	lockfileExclusive       = 0x00000002
	lockfileFailImmediately = 0x00000001
)

var (
	lockMu     sync.Mutex
	lockHandle windows.Handle
	lockFile   string
)

func singletonLockPath() string {
	return filepath.Join(appDataDir(), "instance.lock")
}

// acquireSingletonLock 非阻塞地尝试拿独占锁。返回 ok=true 表示拿到。
// 进程崩溃 / 被 kill 时 Windows 内核会自动释放，不会留死锁。
func acquireSingletonLock() (release func(), ok bool, err error) {
	lockMu.Lock()
	defer lockMu.Unlock()
	// Option B: 同进程内只允许一个持有者；再次尝试视为失败（调用方 bug）。
	if lockHandle != 0 {
		return nil, false, nil
	}

	p := singletonLockPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, false, fmt.Errorf("mkdir lock dir: %w", err)
	}

	utf16, _ := windows.UTF16PtrFromString(p)
	h, openErr := windows.CreateFile(
		utf16,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if openErr != nil {
		return nil, false, fmt.Errorf("open lock file: %w", openErr)
	}

	var overlapped windows.Overlapped
	lockEx := syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	r1, _, lockErr := lockEx.Call(
		uintptr(h),
		uintptr(lockfileExclusive|lockfileFailImmediately),
		0,
		uintptr(0xFFFFFFFF), uintptr(0xFFFFFFFF),
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		windows.CloseHandle(h)
		if errno, okCast := lockErr.(syscall.Errno); okCast && errno == 33 {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("LockFileEx: %v", lockErr)
	}

	lockHandle = h
	lockFile = p
	return func() { releaseSingletonLock() }, true, nil
}

func waitForSingletonLock(timeout time.Duration) (release func(), err error) {
	deadline := time.Now().Add(timeout)
	for {
		rel, ok, err := acquireSingletonLock()
		if ok {
			return rel, nil
		}
		if err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("acquire singleton lock timeout after %s", timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func releaseSingletonLock() {
	lockMu.Lock()
	defer lockMu.Unlock()
	releaseSingletonLockLocked()
}

func releaseSingletonLockLocked() {
	if lockHandle == 0 {
		return
	}
	unlockEx := syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
	var overlapped windows.Overlapped
	unlockEx.Call(
		uintptr(lockHandle),
		0,
		uintptr(0xFFFFFFFF), uintptr(0xFFFFFFFF),
		uintptr(unsafe.Pointer(&overlapped)),
	)
	windows.CloseHandle(lockHandle)
	lockHandle = 0
}
