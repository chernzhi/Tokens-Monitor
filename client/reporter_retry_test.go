package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestReporterFlushRetriesUntilOK 模拟服务端前两次 503、第三次 200，验证重试后仍能入库。
func TestReporterFlushRetriesUntilOK(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/collect" {
			http.NotFound(w, r)
			return
		}
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	cfg := &Config{
		ServerURL: ts.URL,
		UserName:  "t", UserID: "u", Port: 18090,
	}
	rp := NewReporter(cfg)
	rp.Add(UsageRecord{Vendor: "x", Model: "m", TotalTokens: 10})
	rp.Flush()

	if atomic.LoadInt32(&attempts) != 3 {
		t.Fatalf("attempts=%d want 3", attempts)
	}
	if rp.Stats.TotalReported.Load() != 1 {
		t.Fatalf("TotalReported=%d", rp.Stats.TotalReported.Load())
	}
}

// TestReporterFlushStopsRetryOn401 验证身份错误时不重试，避免刷屏。
func TestReporterFlushStopsRetryOn401(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid_token"}`))
	}))
	defer ts.Close()

	cfg := &Config{
		ServerURL: ts.URL,
		UserName:  "t", UserID: "u", Port: 18090,
		AuthToken: "stale-token",
	}
	rp := NewReporter(cfg)
	rp.Add(UsageRecord{Vendor: "x", Model: "m", TotalTokens: 10})
	rp.Flush()

	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Fatalf("attempts=%d want 1 (no retry on 401)", n)
	}
	if rp.Stats.TotalReported.Load() != 0 {
		t.Fatalf("TotalReported should be 0 on auth failure")
	}
}

// TestReporterFallbackToDirectWhenProxyDead 验证：当 cfg 配了一个根本连不上的
// upstream_proxy，但服务端可直连时，reporter 应能自动切到直连成功上报，
// 否则就会出现"机器没装代理但 config 残留死代理 → 上报永久失败"的场景。
func TestReporterFallbackToDirectWhenProxyDead(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	// 127.0.0.1:1 一定连不上，模拟 config 里残留的死代理。
	cfg := &Config{
		ServerURL:     ts.URL,
		UserName:      "t",
		UserID:        "u",
		Port:          18090,
		UpstreamProxy: "http://127.0.0.1:1",
		ReportProxy:   "upstream",
	}
	rp := NewReporter(cfg)
	if !rp.usingProxy {
		t.Fatalf("expect reporter detects it is using a proxy")
	}
	rp.Add(UsageRecord{Vendor: "x", Model: "m", TotalTokens: 10})
	rp.Flush()

	if rp.Stats.TotalReported.Load() != 1 {
		t.Fatalf("TotalReported=%d want 1 (direct fallback should succeed)", rp.Stats.TotalReported.Load())
	}
	if !rp.fellbackToDirect.Load() {
		t.Fatalf("expect fellbackToDirect=true after dead-proxy recovery")
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatalf("server did not receive direct request")
	}
}
