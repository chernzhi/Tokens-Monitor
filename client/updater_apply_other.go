//go:build !windows

package main

import (
	"errors"
)

func renderUpdaterBat() string { return "" }

func (u *Updater) ApplyUpdate(info *ReleaseInfo) error {
	return errors.New("当前平台暂不支持一键更新（仅 Windows）")
}

func RunApplySwap(target, backup, oldPID, logPath string) {}

func PostUpdateCleanup(backupPath string) {}
