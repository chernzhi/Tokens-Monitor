package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// logCapture mirrors everything written to os.Stdout / os.Stderr / log into
// (a) an in-memory ring buffer (for "history" requests) and
// (b) any live subscribers (for SSE streaming to the embedded WebView2).
//
// When ai-monitor is built with -H=windowsgui there is no console window, so
// the original stdout/stderr handles are invalid and writes there are silent
// no-ops. With a console (dev builds) we still tee to the original handles.
type logCapture struct {
	mu       sync.Mutex
	ring     []logLine
	ringHead int
	ringFull bool
	maxLines int
	seq      uint64
	subs     map[*logSubscriber]struct{}

	origStdout *os.File
	origStderr *os.File
}

type logLine struct {
	Seq  uint64 `json:"seq"`
	Time string `json:"time"`
	Text string `json:"text"`
}

type logSubscriber struct {
	ch chan logLine
}

var globalLogCapture *logCapture

// initLogCapture installs pipes over os.Stdout / os.Stderr so anything any
// goroutine prints (including log.Printf, fmt.Println, third-party libs)
// flows through us. Must be called once, very early in main.
func initLogCapture(maxLines int) *logCapture {
	c := &logCapture{
		maxLines:   maxLines,
		ring:       make([]logLine, maxLines),
		subs:       make(map[*logSubscriber]struct{}),
		origStdout: os.Stdout,
		origStderr: os.Stderr,
	}

	rOut, wOut, err := os.Pipe()
	if err != nil {
		return c // best effort; without capture we still run normally
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		rOut.Close()
		wOut.Close()
		return c
	}

	os.Stdout = wOut
	os.Stderr = wErr
	log.SetOutput(wErr) // stdlib log defaults to stderr

	go c.pump(rOut, c.origStdout)
	go c.pump(rErr, c.origStderr)

	globalLogCapture = c
	return c
}

// pump reads lines from r, appends to ring buffer / broadcasts to subscribers,
// and tees to mirror (the original console handle) if mirror is non-nil.
func (c *logCapture) pump(r *os.File, mirror *os.File) {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			c.append(line)
			if mirror != nil {
				_, _ = mirror.WriteString(line)
			}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
	}
}

func (c *logCapture) append(text string) {
	// Strip trailing newline for storage; SSE / JSON consumers add their own.
	for len(text) > 0 && (text[len(text)-1] == '\n' || text[len(text)-1] == '\r') {
		text = text[:len(text)-1]
	}
	if text == "" {
		return
	}

	c.mu.Lock()
	c.seq++
	ln := logLine{
		Seq:  c.seq,
		Time: time.Now().Format("15:04:05.000"),
		Text: text,
	}
	c.ring[c.ringHead] = ln
	c.ringHead = (c.ringHead + 1) % c.maxLines
	if c.ringHead == 0 {
		c.ringFull = true
	}
	subs := make([]*logSubscriber, 0, len(c.subs))
	for s := range c.subs {
		subs = append(subs, s)
	}
	c.mu.Unlock()

	for _, s := range subs {
		select {
		case s.ch <- ln:
		default:
			// subscriber too slow; drop to keep the pipe pump moving
		}
	}
}

// snapshot returns the buffered backlog in chronological order.
func (c *logCapture) snapshot() []logLine {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []logLine
	if c.ringFull {
		out = append(out, c.ring[c.ringHead:]...)
		out = append(out, c.ring[:c.ringHead]...)
	} else {
		out = append(out, c.ring[:c.ringHead]...)
	}
	return out
}

func (c *logCapture) subscribe() *logSubscriber {
	s := &logSubscriber{ch: make(chan logLine, 256)}
	c.mu.Lock()
	c.subs[s] = struct{}{}
	c.mu.Unlock()
	return s
}

func (c *logCapture) unsubscribe(s *logSubscriber) {
	c.mu.Lock()
	delete(c.subs, s)
	c.mu.Unlock()
	close(s.ch)
}

// handleLogsHistory returns the buffered backlog as JSON. Query param `since`
// (uint64 seq) trims entries already seen by the client.
func handleLogsHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if globalLogCapture == nil {
		w.Write([]byte("[]"))
		return
	}
	var since uint64
	if s := r.URL.Query().Get("since"); s != "" {
		since, _ = strconv.ParseUint(s, 10, 64)
	}
	all := globalLogCapture.snapshot()
	out := make([]logLine, 0, len(all))
	for _, ln := range all {
		if ln.Seq > since {
			out = append(out, ln)
		}
	}
	json.NewEncoder(w).Encode(out)
}

// handleLogsStream is a Server-Sent Events endpoint that pushes each new log
// line as it arrives. The browser EventSource auto-reconnects on disconnect;
// we use the `id:` field so reconnection can use Last-Event-ID to skip dupes.
func handleLogsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if globalLogCapture == nil {
		http.Error(w, "log capture not initialized", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Replay any lines the client missed (after Last-Event-ID).
	var since uint64
	if lid := r.Header.Get("Last-Event-ID"); lid != "" {
		since, _ = strconv.ParseUint(lid, 10, 64)
	}
	for _, ln := range globalLogCapture.snapshot() {
		if ln.Seq > since {
			writeSSE(w, ln)
		}
	}
	flusher.Flush()

	sub := globalLogCapture.subscribe()
	defer globalLogCapture.unsubscribe(sub)

	ctx := r.Context()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ln, ok := <-sub.ch:
			if !ok {
				return
			}
			writeSSE(w, ln)
			flusher.Flush()
		case <-heartbeat.C:
			// SSE comment frame keeps the TCP / proxy chain warm.
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, ln logLine) {
	b, err := json.Marshal(ln)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ln.Seq, b)
}
