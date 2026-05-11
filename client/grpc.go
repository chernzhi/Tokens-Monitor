package main

import (
	"encoding/binary"
	"strings"
	"unicode"
	"unicode/utf8"
)

// extractGRPCTextBytes 解析 gRPC over HTTP/2 的流式响应体，提取 protobuf 长度限定字段中
// 看起来像生成文本的内容，返回总字节数。
//
// 为何比 body_bytes/4 精确得多：
//   - 排除每帧 5 字节的 gRPC 帧头（Cursor StreamChat 每个 token chunk 一帧，帧头开销巨大）
//   - 排除 proto 字段标记（varint）和长度前缀
//   - 排除二进制字段（UUID、request-id、内部状态 proto）
//   - 只保留可打印的 UTF-8 文本，即 AI 实际生成的 token 内容
//
// 对于 Cursor AiService/Chat 流式响应，每个 proto frame 大致是：
//
//	[field_tag=1][len][文本delta]   ← 每块只含一个文本字段
//
// 用 text_bytes/4 估算 completion tokens，误差从 ±50% 降至 ±20%。
// 同样用于解析 gRPC 请求体，提取对话文本作为 prompt tokens 估算基准。
func extractGRPCTextBytes(data []byte) int {
	total := 0
	pos := 0
	for pos+5 <= len(data) {
		// gRPC length-prefix frame: 1-byte compression flag + 4-byte big-endian length
		compressed := data[pos]
		msgLen := int(binary.BigEndian.Uint32(data[pos+1 : pos+5]))
		pos += 5

		if msgLen == 0 {
			continue
		}
		if pos+msgLen > len(data) {
			break
		}
		frame := data[pos : pos+msgLen]
		pos += msgLen

		// 压缩帧极少出现（Cursor 未开启 gRPC 压缩），跳过避免解析乱码
		if compressed != 0 {
			continue
		}
		total += protoTextBytes(frame)
	}
	return total
}

// protoTextBytes 遍历一个 protobuf 消息的 wire format，累加所有看起来是
// 生成文本（而非二进制 ID、嵌套 proto）的长度限定字段字节数。
func protoTextBytes(data []byte) int {
	total := 0
	i := 0
	for i < len(data) {
		tag, n := protoVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		wireType := tag & 0x7

		switch wireType {
		case 0: // varint
			_, n = protoVarint(data[i:])
			if n == 0 {
				return total
			}
			i += n
		case 1: // 64-bit fixed
			if i+8 > len(data) {
				return total
			}
			i += 8
		case 2: // length-delimited（string / bytes / embedded message）
			slen, n := protoVarint(data[i:])
			if n == 0 {
				return total
			}
			i += n
			l := int(slen)
			if l < 0 || i+l > len(data) {
				return total
			}
			s := data[i : i+l]
			i += l
			if protoLooksLikeText(s) {
				total += l
			}
		case 5: // 32-bit fixed
			if i+4 > len(data) {
				return total
			}
			i += 4
		default:
			// 未知 wire type，停止解析当前帧，避免因数据损坏进入无限循环
			return total
		}
	}
	return total
}

// protoVarint 解码 protobuf varint，返回 (value, bytesRead)。
// 错误（越界/溢出）时返回 (0, 0)。
func protoVarint(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, c := range b {
		if i >= 10 {
			return 0, 0 // 超过 10 字节 → 溢出
		}
		x |= uint64(c&0x7f) << s
		if c < 0x80 {
			return x, i + 1
		}
		s += 7
	}
	return 0, 0
}

// protoLooksLikeText 判断字节切片是否像 AI 生成的文本（而非 UUID、二进制 ID 或嵌套 proto）。
// 条件：合法 UTF-8，且可打印字符比例 ≥ 85%。
func protoLooksLikeText(b []byte) bool {
	if len(b) < 1 {
		return false
	}
	if !utf8.Valid(b) {
		return false
	}
	if protoLooksLikeIdentifierText(string(b)) {
		return false
	}
	printable, total := 0, 0
	for _, r := range string(b) {
		total++
		if unicode.IsPrint(r) || r == '\n' || r == '\t' || r == '\r' {
			printable++
		}
	}
	if total == 0 {
		return false
	}
	return float64(printable)/float64(total) >= 0.85
}

func protoLooksLikeIdentifierText(text string) bool {
	text = strings.TrimSpace(text)
	if len(text) < 24 || strings.ContainsAny(text, " \t\r\n") {
		return false
	}
	if isCanonicalUUIDText(text) || isLongHexIdentifierText(text) {
		return true
	}
	if strings.ContainsAny(text, "+/=") && isBase64IdentifierText(text) {
		return true
	}
	return false
}

func isCanonicalUUIDText(text string) bool {
	if len(text) != 36 {
		return false
	}
	for i, r := range text {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !isASCIIHex(r) {
				return false
			}
		}
	}
	return true
}

func isLongHexIdentifierText(text string) bool {
	hexDigits := 0
	for _, r := range text {
		if r == '-' || r == '_' {
			continue
		}
		if !isASCIIHex(r) {
			return false
		}
		hexDigits++
	}
	return hexDigits >= 24
}

func isBase64IdentifierText(text string) bool {
	for _, r := range text {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			continue
		}
		return false
	}
	return len(text) >= 32
}

func isASCIIHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
