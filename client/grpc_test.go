package main

import (
	"encoding/binary"
	"testing"
)

// buildGRPCFrame 构造一个未压缩的 gRPC length-prefix 帧。
func buildGRPCFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0 // compression flag: not compressed
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

// buildProtoString 构造 wire type 2 (length-delimited) 的 proto 字段。
// fieldNum 从 1 开始；wire type = 2，所以 tag = (fieldNum << 3) | 2
func buildProtoString(fieldNum uint64, s string) []byte {
	tag := (fieldNum << 3) | 2
	b := appendVarint(nil, tag)
	b = appendVarint(b, uint64(len(s)))
	b = append(b, s...)
	return b
}

// buildProtoVarintField 构造 wire type 0 (varint) 的 proto 字段。
func buildProtoVarintField(fieldNum, val uint64) []byte {
	tag := fieldNum << 3
	b := appendVarint(nil, tag)
	b = appendVarint(b, val)
	return b
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	b = append(b, byte(v))
	return b
}

// TestExtractGRPCTextBytes_SingleFrame 单帧、纯文本 proto 字段。
func TestExtractGRPCTextBytes_SingleFrame(t *testing.T) {
	text := "Hello, world!"
	payload := buildProtoString(1, text)
	data := buildGRPCFrame(payload)

	got := extractGRPCTextBytes(data)
	if got != len(text) {
		t.Fatalf("expected %d text bytes, got %d", len(text), got)
	}
}

// TestExtractGRPCTextBytes_MultiFrame 多帧流式响应：每帧一个 token chunk。
func TestExtractGRPCTextBytes_MultiFrame(t *testing.T) {
	chunks := []string{"Hello", " world", "!\n"}
	var data []byte
	totalText := 0
	for _, c := range chunks {
		data = append(data, buildGRPCFrame(buildProtoString(1, c))...)
		totalText += len(c)
	}

	got := extractGRPCTextBytes(data)
	if got != totalText {
		t.Fatalf("expected %d text bytes across %d frames, got %d", totalText, len(chunks), got)
	}
}

// TestExtractGRPCTextBytes_CompressedFrameSkipped 压缩帧应跳过。
func TestExtractGRPCTextBytes_CompressedFrameSkipped(t *testing.T) {
	text := "compressed content"
	payload := buildProtoString(1, text)
	frame := make([]byte, 5+len(payload))
	frame[0] = 1 // compression flag: compressed
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)

	got := extractGRPCTextBytes(frame)
	if got != 0 {
		t.Fatalf("compressed frame should yield 0, got %d", got)
	}
}

// TestExtractGRPCTextBytes_BinaryFieldSkipped 二进制字段（UUID/随机字节）不应计入文本。
func TestExtractGRPCTextBytes_BinaryFieldSkipped(t *testing.T) {
	// 32 字节随机二进制（非合法 UTF-8）
	binary32 := make([]byte, 32)
	for i := range binary32 {
		binary32[i] = byte(0x80 + i)
	}
	// 构造 wire type 2 字段（直接拼，不用 buildProtoString）
	tag := appendVarint(nil, (1<<3)|2)
	lenPfx := appendVarint(nil, uint64(len(binary32)))
	payload := append(append(tag, lenPfx...), binary32...)
	data := buildGRPCFrame(payload)

	got := extractGRPCTextBytes(data)
	if got != 0 {
		t.Fatalf("binary field should yield 0, got %d", got)
	}
}

// TestExtractGRPCTextBytes_MixedFields 混合字段：文本 + varint + 二进制。
func TestExtractGRPCTextBytes_MixedFields(t *testing.T) {
	text := "the actual delta"
	var payload []byte
	payload = append(payload, buildProtoString(1, text)...)    // 文本字段（计入）
	payload = append(payload, buildProtoVarintField(2, 42)...) // varint 字段（跳过）
	// 添加二进制字段（16 字节，非 UTF-8）
	binBytes := make([]byte, 16)
	for i := range binBytes {
		binBytes[i] = 0xff
	}
	tag := appendVarint(nil, (3<<3)|2)
	lenPfx := appendVarint(nil, uint64(len(binBytes)))
	payload = append(payload, append(append(tag, lenPfx...), binBytes...)...)

	data := buildGRPCFrame(payload)
	got := extractGRPCTextBytes(data)
	if got != len(text) {
		t.Fatalf("expected %d (text only), got %d", len(text), got)
	}
}

// TestExtractGRPCTextBytes_EmptyBody 空/过短的 body 返回 0。
func TestExtractGRPCTextBytes_EmptyBody(t *testing.T) {
	if got := extractGRPCTextBytes(nil); got != 0 {
		t.Fatalf("nil body: expected 0, got %d", got)
	}
	if got := extractGRPCTextBytes([]byte{0x00, 0x00}); got != 0 {
		t.Fatalf("too-short body: expected 0, got %d", got)
	}
}

// TestExtractGRPCTextBytes_ZeroLengthFrame 长度为 0 的帧（keepalive ping）应跳过。
func TestExtractGRPCTextBytes_ZeroLengthFrame(t *testing.T) {
	ping := []byte{0x00, 0x00, 0x00, 0x00, 0x00} // 5-byte header, msgLen=0
	text := "after ping"
	data := append(ping, buildGRPCFrame(buildProtoString(1, text))...)
	got := extractGRPCTextBytes(data)
	if got != len(text) {
		t.Fatalf("expected %d, got %d", len(text), got)
	}
}

// TestProtoLooksLikeText 边界条件验证。
func TestProtoLooksLikeText(t *testing.T) {
	if !protoLooksLikeText([]byte("Hello, 世界!\n")) {
		t.Fatal("printable UTF-8 should look like text")
	}
	if protoLooksLikeText([]byte{0x80, 0x81, 0x82}) {
		t.Fatal("invalid UTF-8 should not look like text")
	}
	if protoLooksLikeText(nil) {
		t.Fatal("empty slice should not look like text")
	}
	if protoLooksLikeText([]byte("550e8400-e29b-41d4-a716-446655440000")) {
		t.Fatal("UUID-like identifier should not be counted as generated text")
	}
	if protoLooksLikeText([]byte("MkQob/sjJDPXH6jdbnxGxNiF84fMnrZbdVv9Ft2dY6Lud0BM=")) {
		t.Fatal("base64-like request id should not be counted as generated text")
	}
}

// TestProtoVarint 基础 varint 解码。
func TestProtoVarint(t *testing.T) {
	cases := []struct {
		input []byte
		val   uint64
		n     int
	}{
		{[]byte{0x00}, 0, 1},
		{[]byte{0x01}, 1, 1},
		{[]byte{0x7f}, 127, 1},
		{[]byte{0x80, 0x01}, 128, 2},
		{[]byte{0x96, 0x01}, 150, 2},
	}
	for _, c := range cases {
		v, n := protoVarint(c.input)
		if v != c.val || n != c.n {
			t.Errorf("protoVarint(%v) = (%d, %d), want (%d, %d)", c.input, v, n, c.val, c.n)
		}
	}
	// 越界/溢出
	if _, n := protoVarint(nil); n != 0 {
		t.Fatal("nil: expected n=0")
	}
	overflow := make([]byte, 11)
	for i := range overflow {
		overflow[i] = 0x80
	}
	if _, n := protoVarint(overflow); n != 0 {
		t.Fatal("overflow: expected n=0")
	}
}
