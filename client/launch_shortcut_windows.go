//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"os"
)

// resolveShortcutTarget 解析 Windows .lnk 快捷方式，取出其目标 exe 路径。
// 仅解析 Shell Link Binary 格式里稳定的 LinkInfo.LocalBasePath（ANSI），
// 不依赖 COM / 第三方库；解析失败返回 ("", false)。
//
// 参考 [MS-SHLLINK]：
//   - ShellLinkHeader 固定 0x4C 字节，偏移 20 处是 LinkFlags(uint32 LE)。
//   - LinkFlags bit0 HasLinkTargetIDList：紧跟一个 2 字节大小的 IDList，需跳过。
//   - LinkFlags bit1 HasLinkInfo：之后是 LinkInfo 结构。
//   - LinkInfo 偏移 16 处是 LocalBasePathOffset(uint32)，指向 LinkInfo 内
//     一个以 NUL 结尾的 ANSI 路径字符串。
func resolveShortcutTarget(lnkPath string) (string, bool) {
	data, err := os.ReadFile(lnkPath)
	if err != nil || len(data) < 0x4C {
		return "", false
	}
	const (
		headerSize          = 0x4C
		flagHasIDList       = 0x00000001
		flagHasLinkInfo     = 0x00000002
		localBasePathOffPos = 16
	)
	flags := binary.LittleEndian.Uint32(data[20:24])
	pos := headerSize

	if flags&flagHasIDList != 0 {
		if pos+2 > len(data) {
			return "", false
		}
		idListSize := int(binary.LittleEndian.Uint16(data[pos : pos+2]))
		pos += 2 + idListSize
	}
	if flags&flagHasLinkInfo == 0 || pos+20 > len(data) {
		return "", false
	}

	linkInfoStart := pos
	localBasePathOffset := int(binary.LittleEndian.Uint32(data[linkInfoStart+localBasePathOffPos : linkInfoStart+localBasePathOffPos+4]))
	if localBasePathOffset == 0 {
		return "", false
	}
	strStart := linkInfoStart + localBasePathOffset
	if strStart < 0 || strStart >= len(data) {
		return "", false
	}
	end := bytes.IndexByte(data[strStart:], 0)
	if end <= 0 {
		return "", false
	}
	return string(data[strStart : strStart+end]), true
}
