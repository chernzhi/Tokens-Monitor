package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxQueueSize = 10000

// 上报重试次数（瞬时网络抖动、服务端短暂重启）
const reportMaxAttempts = 4

// UsageRecord is a single token usage event to be reported to the server.
type UsageRecord struct {
	ClientID         string  `json:"client_id"`
	UserName         string  `json:"user_name"`
	UserID           string  `json:"user_id"`
	Department       string  `json:"department"`
	RequestID        string  `json:"request_id,omitempty"`
	SourceApp        string  `json:"source_app,omitempty"`
	Vendor           string  `json:"vendor"`
	Model            string  `json:"model"`
	Endpoint         string  `json:"endpoint"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostMultiplier   float64 `json:"cost_multiplier,omitempty"`
	RequestTime      string  `json:"request_time"`
	// Source 上报来源：client 为 JSON 解析；client-mitm-estimate 为 gRPC/二进制体积估算。
	Source string `json:"source,omitempty"`
}

// ReporterStats tracks cumulative reporting statistics.
type ReporterStats struct {
	TotalReported atomic.Int64
	TotalTokens   atomic.Int64
	TotalFailed   atomic.Int64
}

// Reporter batches usage records and periodically sends them to the central server.
type Reporter struct {
	cfg       *Config
	clientID  string
	sourceApp string
	queue     []UsageRecord
	mu        sync.Mutex
	client    *http.Client
	// directClient 永远走直连，作为 client 走不通时的 fallback。
	// 关键场景：用户机器从未配过代理，但 config.json 里残留了
	// 错误的 upstream_proxy（例如 socks5://127.0.0.1:7890），
	// 否则每次上报都因 dial 拒绝而失败、又看不到日志。
	directClient *http.Client
	// fellbackToDirect 在某次请求触发直连 fallback 成功后置位，
	// 后续直接用 directClient，避免每次都先试 dead 上游再退避。
	fellbackToDirect atomic.Bool
	usingProxy       bool // 启动时是否带了代理（用于日志）
	Stats            ReporterStats
	heartbeatOK      sync.Once
	// authWarnedAt 记录上次警告时间，节流：同一身份问题 5 分钟内只提示 / 弹一次 wizard。
	// 以前用 sync.Once 一辈子只提示一次，导致用户错过首次 wizard 后再也看不到提醒。
	authWarnMu   sync.Mutex
	authWarnedAt time.Time
	// OnAuthFailed 在身份凭证失败时触发（受 5 分钟节流），用于推到登录向导。
	OnAuthFailed func()
}

// resolveReportProxy determines the proxy function for the Reporter HTTP client.
// Auto mode uses an explicit/detected upstream when available, otherwise direct.
// Loopback upstream candidates are probed before use so stale local developer proxies are ignored.
func resolveReportProxy(cfg *Config) func(*http.Request) (*url.URL, error) {
	directProxy := func(*http.Request) (*url.URL, error) { return nil, nil }

	if cfg == nil {
		return directProxy
	}

	mode := strings.TrimSpace(strings.ToLower(cfg.ReportProxy))

	switch mode {
	case "direct":
		log.Println("[reporter] 上报路由: 直连 (report_proxy=direct)")
		return directProxy

	case "upstream":
		upstream := strings.TrimSpace(cfg.UpstreamProxy)
		if upstream == "" {
			upstream = detectUpstreamProxy(cfg)
		}
		if upstream != "" {
			if u, err := url.Parse(upstream); err == nil {
				log.Printf("[reporter] 上报路由: 走上游代理 %s (report_proxy=upstream)", u.Redacted())
				return http.ProxyURL(u)
			}
		}
		log.Println("[reporter] 上报路由: report_proxy=upstream 但未找到上游代理，回退直连")
		return directProxy

	case "", "auto":
		upstream := strings.TrimSpace(cfg.UpstreamProxy)
		if upstream == "" {
			upstream = detectUpstreamProxy(cfg)
		}
		if upstream != "" {
			if u, err := url.Parse(upstream); err == nil {
				log.Printf("[reporter] 上报路由: 走上游代理 %s (report_proxy=auto)", u.Redacted())
				return http.ProxyURL(u)
			}
		}
		log.Println("[reporter] 上报路由: 直连 (report_proxy=auto, no upstream)")
		return directProxy

	default:
		if u, err := url.Parse(mode); err == nil && u.Host != "" {
			log.Printf("[reporter] 上报路由: 走指定代理 %s", u.Redacted())
			return http.ProxyURL(u)
		}
		log.Printf("[reporter] 上报路由: report_proxy=%q 无法解析，回退直连", mode)
		return directProxy
	}
}

// isPrivateServerURL checks if the server URL points to a private/intranet address.
func isPrivateServerURL(serverURL string) bool {
	u, err := url.Parse(serverURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	privateRanges := []struct{ start, end net.IP }{
		{net.ParseIP("10.0.0.0"), net.ParseIP("10.255.255.255")},
		{net.ParseIP("172.16.0.0"), net.ParseIP("172.31.255.255")},
		{net.ParseIP("192.168.0.0"), net.ParseIP("192.168.255.255")},
		{net.ParseIP("127.0.0.0"), net.ParseIP("127.255.255.255")},
		{net.ParseIP("169.254.0.0"), net.ParseIP("169.254.255.255")},
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	for _, r := range privateRanges {
		if bytes.Compare(ip4, r.start.To4()) >= 0 && bytes.Compare(ip4, r.end.To4()) <= 0 {
			return true
		}
	}
	return false
}

func newHTTPClientForReporter(cfg *Config) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:                 resolveReportProxy(cfg),
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// newDirectHTTPClientForReporter 总是直连，无视任何代理配置。
// 用作主 client 走不通时的兜底，避免「config 残留死代理 → 上报永久失败」。
func newDirectHTTPClientForReporter() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func NewReporter(cfg *Config) *Reporter {
	hostname, _ := os.Hostname()
	clientID := cfg.UserID + "@" + hostname

	// 判定主 client 是否实际带代理：仅当 cfg 解析后的代理 func 对一个虚构 URL 返回非 nil。
	usingProxy := false
	if pf := resolveReportProxy(cfg); pf != nil {
		probe, _ := http.NewRequest(http.MethodGet, cfg.ServerURL+"/health", nil)
		if probe != nil {
			if u, err := pf(probe); err == nil && u != nil {
				usingProxy = true
			}
		}
	}

	return &Reporter{
		cfg:          cfg,
		clientID:     clientID,
		client:       newHTTPClientForReporter(cfg),
		directClient: newDirectHTTPClientForReporter(),
		usingProxy:   usingProxy,
	}
}

// PingServer 启动时探测上报服务是否可达（GET /health）。
// 若主 client 走代理失败而直连可达，自动切到 directClient 并保留这个状态，
// 这样后续 collect / heartbeat 不会再卡在 dead 上游代理上。
func (r *Reporter) PingServer(ctx context.Context) error {
	doPing := func(c *http.Client) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.cfg.ServerURL+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := c.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	}

	if err := doPing(r.pickClient()); err != nil {
		// 已经在直连，没什么可退的，直接返回。
		if r.fellbackToDirect.Load() || !r.usingProxy {
			return err
		}
		if !isProxyTransportError(err) {
			return err
		}
		// 主代理疑似不可达，临时换直连重试一次。
		if err2 := doPing(r.directClient); err2 == nil {
			r.fellbackToDirect.Store(true)
			log.Printf("[上报] 上游代理不可达 (%v)，已自动切换为直连上报 %s", err, r.cfg.ServerURL)
			return nil
		} else {
			return err
		}
	}
	return nil
}

// Add enqueues a usage record for reporting.
func (r *Reporter) Add(record UsageRecord) {
	record.ClientID = r.clientID
	record.UserName = r.cfg.UserName
	record.UserID = r.cfg.UserID
	record.Department = r.cfg.Department
	if record.SourceApp == "" {
		record.SourceApp = r.sourceApp
	}
	record.RequestTime = time.Now().Format(time.RFC3339)
	if record.RequestID == "" {
		record.RequestID = newRequestID()
	}

	r.mu.Lock()
	if len(r.queue) >= maxQueueSize {
		r.queue = r.queue[1:]
		log.Printf("[警告] 队列已满(%d)，丢弃最早的记录", maxQueueSize)
	}
	r.queue = append(r.queue, record)
	r.mu.Unlock()

	if record.Source == "client-mitm-estimate" {
		log.Printf("[记录·估算] %s | %s | 输入:%d 输出:%d 总计:%d（响应非 JSON，按体积粗算，非官方计费）",
			record.Vendor, record.Model,
			record.PromptTokens, record.CompletionTokens, record.TotalTokens)
	} else {
		log.Printf("[记录] %s | %s | 输入:%d 输出:%d 总计:%d",
			record.Vendor, record.Model,
			record.PromptTokens, record.CompletionTokens, record.TotalTokens)
	}
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
}

// Start begins the periodic reporting loop. Should be called in a goroutine.
// Exits when ctx is cancelled, performing a final Flush.
func (r *Reporter) Start(ctx context.Context) {
	r.sendHeartbeatWithRetry()

	flushTicker := time.NewTicker(30 * time.Second)
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer flushTicker.Stop()
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.Flush()
			return
		case <-flushTicker.C:
			r.Flush()
		case <-heartbeatTicker.C:
			r.sendHeartbeatWithRetry()
		}
	}
}

// postJSONRetry POST JSON 并在失败时指数退避重试。
// 若主 client 因代理 dial / 连接级错误失败，会即时改用 directClient 重试，
// 第一次成功后将 reporter 永久切到直连，避免后续每次都先试坏代理再退避。
func (r *Reporter) postJSONRetry(path string, body []byte) (*http.Response, error) {
	full := r.cfg.ServerURL + path
	var lastErr error
	for attempt := 0; attempt < reportMaxAttempts; attempt++ {
		if attempt > 0 {
			d := time.Duration(400*(1<<uint(attempt-1))) * time.Millisecond
			if d > 3*time.Second {
				d = 3 * time.Second
			}
			time.Sleep(d)
		}
		req, err := http.NewRequest(http.MethodPost, full, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		if r.cfg.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.AuthToken)
		} else if r.cfg.APIKey != "" {
			req.Header.Set("X-API-Key", r.cfg.APIKey)
		}
		client := r.pickClient()
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			// 仅在还在用代理时尝试直连兜底；已经直连过就别再重复探测。
			if r.usingProxy && !r.fellbackToDirect.Load() && isProxyTransportError(err) {
				if resp2, err2 := r.directClient.Do(cloneRequestWithBody(req, body)); err2 == nil {
					r.fellbackToDirect.Store(true)
					log.Printf("[上报] 上游代理不可达 (%v)，已切到直连，本次请求经直连成功", err)
					if resp2.StatusCode == http.StatusOK {
						return resp2, nil
					}
					b, _ := io.ReadAll(resp2.Body)
					resp2.Body.Close()
					lastErr = fmt.Errorf("HTTP %d: %s", resp2.StatusCode, string(b))
					if resp2.StatusCode == http.StatusUnauthorized || resp2.StatusCode == http.StatusForbidden {
						r.maybeWarnAuth(resp2.StatusCode, b)
						return nil, lastErr
					}
					if resp2.StatusCode >= 400 && resp2.StatusCode < 500 &&
						resp2.StatusCode != http.StatusRequestTimeout &&
						resp2.StatusCode != http.StatusTooManyRequests {
						return nil, lastErr
					}
					continue
				}
			}
			log.Printf("[网络] POST %s 第 %d/%d 次失败: %v", path, attempt+1, reportMaxAttempts, err)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
		// 401/403 是身份凭证问题，重试无意义；立即返回并一次性给出可操作指引。
		// 其他 4xx（除 408/429 外）同样是客户端错误，也不应反复重试。
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			r.maybeWarnAuth(resp.StatusCode, b)
			return nil, lastErr
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
			resp.StatusCode != http.StatusRequestTimeout &&
			resp.StatusCode != http.StatusTooManyRequests {
			log.Printf("[网络] POST %s HTTP %d（客户端错误，放弃重试）", path, resp.StatusCode)
			return nil, lastErr
		}
		log.Printf("[网络] POST %s 第 %d/%d 次 HTTP %d", path, attempt+1, reportMaxAttempts, resp.StatusCode)
	}
	return nil, lastErr
}

// Flush sends all queued records to the server.
func (r *Reporter) Flush() {
	r.mu.Lock()
	if len(r.queue) == 0 {
		r.mu.Unlock()
		return
	}
	records := r.queue
	r.queue = nil
	r.mu.Unlock()

	data, err := json.Marshal(records)
	if err != nil {
		log.Printf("[上报] 序列化失败: %v", err)
		r.requeue(records)
		return
	}

	resp, err := r.postJSONRetry("/api/collect", data)
	if err != nil {
		log.Printf("[上报] 最终失败: %v (将重试)", err)
		r.Stats.TotalFailed.Add(int64(len(records)))
		r.requeue(records)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	r.Stats.TotalReported.Add(int64(len(records)))
	for _, rec := range records {
		r.Stats.TotalTokens.Add(int64(rec.TotalTokens))
	}
	log.Printf("[上报] 成功 %d 条 → %s (累计: %d 条, %d tokens)",
		len(records), r.cfg.ServerURL, r.Stats.TotalReported.Load(), r.Stats.TotalTokens.Load())
}

func (r *Reporter) requeue(records []UsageRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := len(records) + len(r.queue)
	if total > maxQueueSize {
		overflow := total - maxQueueSize
		if overflow < len(records) {
			records = records[overflow:]
		} else {
			records = nil
		}
	}
	r.queue = append(records, r.queue...)
}

func (r *Reporter) sendHeartbeatWithRetry() {
	hostname, _ := os.Hostname()
	data, err := json.Marshal(map[string]interface{}{
		"client_id":  r.clientID,
		"user_name":  r.cfg.UserName,
		"user_id":    r.cfg.UserID,
		"department": r.cfg.Department,
		"hostname":   hostname,
		"version":    Version,
	})
	if err != nil {
		return
	}

	resp, err := r.postJSONRetry("/api/clients/heartbeat", data)
	if err != nil {
		log.Printf("[心跳] 发送失败: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	r.heartbeatOK.Do(func() {
		log.Printf("[心跳] 已连接上报服务器 %s（此后每 30s 静默心跳）", r.cfg.ServerURL)
	})
}

// maybeWarnAuth 在身份令牌失效时，最多每 5 分钟提示一次并触发 OnAuthFailed。
// 这样既避免心跳每 30s 刷一次重复日志，又保证用户错过首次 wizard 时仍会被再次提醒。
func (r *Reporter) maybeWarnAuth(status int, body []byte) {
	r.authWarnMu.Lock()
	defer r.authWarnMu.Unlock()
	if !r.authWarnedAt.IsZero() && time.Since(r.authWarnedAt) < 5*time.Minute {
		return
	}
	r.authWarnedAt = time.Now()
	if r.cfg.AuthToken != "" {
		log.Printf("[认证] 身份令牌已失效 (HTTP %d): %s", status, string(body))
		log.Printf("[认证] 将自动打开登录向导；也可双击「重新配置.bat」或运行 ai-monitor.exe --setup。")
	} else {
		log.Printf("[认证] 上报被拒绝 (HTTP %d): %s", status, string(body))
		log.Printf("[认证] config.json 缺少有效 auth_token / api_key，将自动打开登录向导。")
	}
	if r.OnAuthFailed != nil {
		go r.OnAuthFailed()
	}
}

// pickClient 根据是否已 fallback 选择 http 客户端。
func (r *Reporter) pickClient() *http.Client {
	if r.fellbackToDirect.Load() && r.directClient != nil {
		return r.directClient
	}
	return r.client
}

// isProxyTransportError 判断 err 是否疑似上游代理 dial 不通 / 链路类失败。
// 条件刻意宽松：dial refused、超时、EOF、TLS 握手错误都算，因为这些
// 都意味着「换个不走代理的路径再试一次」是合理的兜底。
func isProxyTransportError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "proxyconnect") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "tls handshake") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host") {
		return true
	}
	// net.OpError 的 dial 类错误
	var opErr *net.OpError
	if errAs(err, &opErr) {
		if opErr.Op == "dial" || opErr.Op == "proxyconnect" {
			return true
		}
	}
	return false
}

// errAs 是 errors.As 的本地包装，避免在文件顶端再多 import 一个包。
// （等价：errors.As(err, target)）
func errAs(err error, target interface{}) bool {
	type wrapper interface{ Unwrap() error }
	for cur := err; cur != nil; {
		switch t := target.(type) {
		case **net.OpError:
			if v, ok := cur.(*net.OpError); ok {
				*t = v
				return true
			}
		}
		w, ok := cur.(wrapper)
		if !ok {
			return false
		}
		cur = w.Unwrap()
	}
	return false
}

// cloneRequestWithBody 复用已构造的 req 头/方法/URL，重新塞 body 用于二次发送。
// http.Client.Do 会消费 Body；fallback 重发时必须重新塞一份。
func cloneRequestWithBody(orig *http.Request, body []byte) *http.Request {
	clone := orig.Clone(orig.Context())
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	clone.ContentLength = int64(len(body))
	return clone
}
