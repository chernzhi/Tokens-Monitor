//go:build !windows

package main

import "time"

// acquireSingletonLock 在非 Windows 平台是 no-op：返回一个空 release。
// 真正的单例保护仍由现有 checkExistingInstance 提供。
func acquireSingletonLock() (release func(), ok bool, err error) {
	return func() {}, true, nil
}

func waitForSingletonLock(_ time.Duration) (release func(), err error) {
	return func() {}, nil
}

func releaseSingletonLock() {}
