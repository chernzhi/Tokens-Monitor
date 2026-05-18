package main

import (
	"net"
	"testing"
)

func TestDetectCommonLoopbackUpstreamProxySelectsReachablePort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen temp loopback port: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	orig := append([]int(nil), commonLoopbackUpstreamPorts...)
	commonLoopbackUpstreamPorts = []int{port}
	t.Cleanup(func() { commonLoopbackUpstreamPorts = orig })

	got := detectCommonLoopbackUpstreamProxy()
	want := "http://127.0.0.1:" + intToString(port)
	if got != want {
		t.Fatalf("detectCommonLoopbackUpstreamProxy()=%q want %q", got, want)
	}
}

func TestDetectCommonLoopbackUpstreamProxySkipsUnreachable(t *testing.T) {
	orig := append([]int(nil), commonLoopbackUpstreamPorts...)
	commonLoopbackUpstreamPorts = []int{6553}
	t.Cleanup(func() { commonLoopbackUpstreamPorts = orig })

	got := detectCommonLoopbackUpstreamProxy()
	if got != "" {
		t.Fatalf("detectCommonLoopbackUpstreamProxy()=%q want empty", got)
	}
}

