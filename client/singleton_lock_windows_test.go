//go:build windows

package main

import (
	"sync"
	"testing"
	"time"
)

func TestAcquireSingletonLock_ExclusiveBetweenInProcessCallers(t *testing.T) {
	// 同进程内 LockFileEx 也是独占的（用不同的 file handle）。
	t.Setenv("APPDATA", t.TempDir())

	release1, ok1, err := acquireSingletonLock()
	if err != nil || !ok1 {
		t.Fatalf("first acquire should succeed: ok=%v err=%v", ok1, err)
	}
	defer release1()

	_, ok2, err := acquireSingletonLock()
	if err != nil {
		t.Fatalf("second acquire returned error: %v", err)
	}
	if ok2 {
		t.Fatalf("second acquire should fail while first holds the lock")
	}
}

func TestAcquireSingletonLock_ReacquireAfterRelease(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	release1, ok1, err := acquireSingletonLock()
	if err != nil || !ok1 {
		t.Fatalf("first acquire failed: %v", err)
	}
	release1()

	release2, ok2, err := acquireSingletonLock()
	if err != nil || !ok2 {
		t.Fatalf("re-acquire after release should succeed: ok=%v err=%v", ok2, err)
	}
	release2()
}

func TestWaitForSingletonLock_TimesOutWhenHeld(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	release1, ok, err := acquireSingletonLock()
	if !ok || err != nil {
		t.Fatalf("setup failed: ok=%v err=%v", ok, err)
	}
	defer release1()

	start := time.Now()
	_, err = waitForSingletonLock(500 * time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout, got nil")
	}
	if elapsed < 400*time.Millisecond {
		t.Fatalf("returned too early: %v", elapsed)
	}
}

func TestWaitForSingletonLock_AcquiresAfterReleased(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	release1, _, _ := acquireSingletonLock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(200 * time.Millisecond)
		release1()
	}()

	release2, err := waitForSingletonLock(2 * time.Second)
	if err != nil {
		t.Fatalf("wait should succeed within timeout: %v", err)
	}
	release2()
	wg.Wait()
}
