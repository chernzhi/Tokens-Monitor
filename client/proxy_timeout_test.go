package main

import (
	"io"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestDialViaUpstreamProxyTimesOutWaitingForConnectResponse(t *testing.T) {
	oldConnectTimeout := upstreamProxyConnectTimeout
	oldBackoff := upstreamProxyRetryInitialBackoff
	upstreamProxyConnectTimeout = 25 * time.Millisecond
	upstreamProxyRetryInitialBackoff = time.Millisecond
	defer func() {
		upstreamProxyConnectTimeout = oldConnectTimeout
		upstreamProxyRetryInitialBackoff = oldBackoff
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	proxyURL := &url.URL{Scheme: "http", Host: ln.Addr().String()}
	server := &ProxyServer{upstreamProxy: proxyURL}
	started := time.Now()
	conn, err := server.dialViaUpstreamProxy("example.com:443")
	if err == nil {
		conn.Close()
		t.Fatal("expected CONNECT response timeout")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("CONNECT timeout took too long: %s", elapsed)
	}
}

func TestCopyConnWithIdleTimeoutReturnsOnIdle(t *testing.T) {
	oldIdleTimeout := proxyTunnelIdleTimeout
	proxyTunnelIdleTimeout = 25 * time.Millisecond
	defer func() { proxyTunnelIdleTimeout = oldIdleTimeout }()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	done := make(chan error, 1)
	go func() {
		_, err := copyConnWithIdleTimeout(serverConn, clientConn)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected idle timeout error")
		}
		if !isTimeoutNetworkError(err) {
			t.Fatalf("expected timeout error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("copy did not return after idle timeout")
	}
}
