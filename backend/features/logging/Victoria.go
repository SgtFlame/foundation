package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StreamFields are the fields VictoriaLogs groups records into streams by.
const StreamFields = "app,instance,source,tenant.id"

// Target is where VictoriaLogs is. The zero Target stores nothing.
type Target struct {
	URL      string // the base URL, e.g. http://localhost:9428
	Username string // for an instance behind an auth proxy
	Password string
}

// Configured tells whether t names an instance.
func (t Target) Configured() bool { return t.URL != "" }

// Do sends req to t, with its credentials when it has them.
func (t Target) Do(client *http.Client, req *http.Request) (*http.Response, error) {
	if t.Username != "" {
		req.SetBasicAuth(t.Username, t.Password)
	}
	return client.Do(req)
}

// VictoriaStatus is how storing is going, for the Log Explorer.
type VictoriaStatus struct {
	Configured  bool
	URL         string // without credentials
	Level       slog.Level
	Queued      int
	Dropped     int64 // records dropped because the queue was full
	LastError   string
	LastErrorAt time.Time
}

// VictoriaOptions tune a Victoria; zero values take the defaults.
type VictoriaOptions struct {
	App, Instance string        // stream fields; "foundation" and the host name
	QueueSize     int           // records held while VictoriaLogs is slow or down; 10000
	BatchSize     int           // records per post; 1000
	FlushEvery    time.Duration // 1s
	Client        *http.Client
	Errors        io.Writer // where a failing or recovered target is reported; stderr
}

// Victoria stores records in VictoriaLogs (documentation/plans/Logging.md ›
// Storage). Its Handler queues each record as a JSON line, and a worker posts
// them to /insert/jsonline in batches. It never blocks a request: when the
// queue is full the oldest records are dropped and counted, and failed posts
// are retried with backoff. Its target and level change live.
type Victoria struct {
	opts VictoriaOptions

	mu      sync.Mutex
	target  Target
	queue   [][]byte
	head    uint64 // the sequence number of queue[0], so a flush removes only what it posted
	lastErr string
	errAt   time.Time
	dropped atomic.Int64
	level   slog.LevelVar
	users   atomic.Pointer[map[string]UserLevel]

	wake    chan struct{}
	stop    chan struct{}
	stopped chan struct{}
}

func NewVictoria(opts VictoriaOptions) *Victoria {
	if opts.App == "" {
		opts.App = "foundation"
	}
	if opts.Instance == "" {
		opts.Instance, _ = os.Hostname()
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 10000
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1000
	}
	if opts.FlushEvery <= 0 {
		opts.FlushEvery = time.Second
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if opts.Errors == nil {
		opts.Errors = os.Stderr
	}
	v := &Victoria{opts: opts, wake: make(chan struct{}, 1), stop: make(chan struct{}), stopped: make(chan struct{})}
	go v.run()
	return v
}

// App and Instance are the stream fields every record from here carries.
func (v *Victoria) App() string      { return v.opts.App }
func (v *Victoria) Instance() string { return v.opts.Instance }

// Handler writes records from the stored level up as JSON lines with
// VictoriaLogs' names and the server's stream fields, and a user's records
// from their own level while it lasts (SetUserLevels). It is disabled while
// there is no target, so nothing is built for it.
func (v *Victoria) Handler() slog.Handler {
	opts := &slog.HandlerOptions{Level: storedLevel{v}, ReplaceAttr: victoriaNames}
	return userLevels{v: v, Handler: slog.NewJSONHandler(queueWriter{v}, opts).WithAttrs([]slog.Attr{
		slog.String("app", v.opts.App), slog.String("instance", v.opts.Instance), slog.String("source", "server"),
	})}
}

// UserLevel is one user's lowest stored level until a time: full logging
// for one user (documentation/plans/Logging.md).
type UserLevel struct {
	Level slog.Level
	Until time.Time
}

// SetUserLevels replaces the users' own levels, by user ID.
func (v *Victoria) SetUserLevels(levels map[string]UserLevel) {
	v.users.Store(&levels)
}

// UserLevel is the user's own level, while it lasts.
func (v *Victoria) UserLevel(user string) (slog.Level, bool) {
	levels := v.users.Load()
	if levels == nil || user == "" {
		return 0, false
	}
	l, ok := (*levels)[user]
	if !ok || time.Now().After(l.Until) {
		return 0, false
	}
	return l.Level, true
}

// userLevels enables a record below the stored level when it belongs to a
// user whose own level allows it.
type userLevels struct {
	slog.Handler
	v *Victoria
}

func (h userLevels) Enabled(ctx context.Context, level slog.Level) bool {
	if h.Handler.Enabled(ctx, level) {
		return true
	}
	if !h.v.Target().Configured() {
		return false
	}
	lowest, ok := h.v.UserLevel(FromContext(ctx).user)
	return ok && level >= lowest
}

func (h userLevels) WithAttrs(attrs []slog.Attr) slog.Handler {
	return userLevels{Handler: h.Handler.WithAttrs(attrs), v: h.v}
}

func (h userLevels) WithGroup(name string) slog.Handler {
	return userLevels{Handler: h.Handler.WithGroup(name), v: h.v}
}

type storedLevel struct{ v *Victoria }

func (l storedLevel) Level() slog.Level {
	if !l.v.Target().Configured() {
		return slog.LevelError + 100
	}
	return l.v.level.Level()
}

// queueWriter enqueues each Write as one line: slog's JSON handler writes
// each record in one call.
type queueWriter struct{ v *Victoria }

func (w queueWriter) Write(p []byte) (int, error) {
	w.v.Enqueue(p)
	return len(p), nil
}

// Enqueue queues one JSON line (with or without its newline) to store, such
// as a record forwarded from a browser.
func (v *Victoria) Enqueue(line []byte) {
	line = bytes.TrimRight(line, "\n")
	if len(line) == 0 {
		return
	}
	entry := make([]byte, len(line)+1)
	copy(entry, line)
	entry[len(line)] = '\n'

	v.mu.Lock()
	if !v.target.Configured() {
		v.mu.Unlock()
		return
	}
	if len(v.queue) >= v.opts.QueueSize {
		drop := len(v.queue) - v.opts.QueueSize + 1
		v.queue = v.queue[drop:]
		v.head += uint64(drop)
		v.dropped.Add(int64(drop))
	}
	v.queue = append(v.queue, entry)
	full := len(v.queue) >= v.opts.BatchSize
	v.mu.Unlock()
	if full {
		v.poke()
	}
}

// SetTarget points storage at t; the zero Target stops it and drops the queue.
func (v *Victoria) SetTarget(t Target) {
	t.URL = strings.TrimRight(t.URL, "/")
	v.mu.Lock()
	changed := v.target != t
	v.target = t
	if !t.Configured() {
		v.head += uint64(len(v.queue))
		v.queue = nil
	}
	if changed {
		v.lastErr, v.errAt = "", time.Time{}
	}
	v.mu.Unlock()
	if changed {
		v.poke()
	}
}

func (v *Victoria) Target() Target {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.target
}

// SetLevel sets the lowest level stored.
func (v *Victoria) SetLevel(level slog.Level) { v.level.Set(level) }

func (v *Victoria) Status() VictoriaStatus {
	v.mu.Lock()
	defer v.mu.Unlock()
	return VictoriaStatus{
		Configured: v.target.Configured(), URL: v.target.URL, Level: v.level.Level(),
		Queued: len(v.queue), Dropped: v.dropped.Load(), LastError: v.lastErr, LastErrorAt: v.errAt,
	}
}

// Close posts what is queued, waiting until ctx ends at most.
func (v *Victoria) Close(ctx context.Context) {
	select {
	case <-v.stop:
		return
	default:
		close(v.stop)
	}
	select {
	case <-v.stopped:
	case <-ctx.Done():
	}
}

func (v *Victoria) poke() {
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

func (v *Victoria) run() {
	defer close(v.stopped)
	ticker := time.NewTicker(v.opts.FlushEvery)
	defer ticker.Stop()
	var retryAt time.Time
	backoff := time.Duration(0)
	for {
		stopping := false
		select {
		case <-v.stop:
			stopping = true
		case <-v.wake:
			retryAt = time.Time{} // a new target or a full batch: try now
		case <-ticker.C:
		}
		for time.Now().After(retryAt) || stopping {
			sent, err := v.flush()
			if err != nil {
				backoff = min(max(2*backoff, time.Second), 30*time.Second)
				retryAt = time.Now().Add(backoff)
				break
			}
			backoff = 0
			if sent < v.opts.BatchSize {
				break
			}
		}
		if stopping {
			return
		}
	}
}

// flush posts up to one batch and removes it from the queue once stored.
func (v *Victoria) flush() (int, error) {
	v.mu.Lock()
	target := v.target
	batch := v.queue[:min(len(v.queue), v.opts.BatchSize)]
	first := v.head
	v.mu.Unlock()
	if len(batch) == 0 || !target.Configured() {
		return 0, nil
	}

	err := v.post(target, batch)
	v.mu.Lock()
	defer v.mu.Unlock()
	if err != nil {
		if v.lastErr == "" {
			fmt.Fprintf(v.opts.Errors, "logging: cannot store logs in VictoriaLogs at %s, retrying: %v\n", target.URL, err)
		}
		v.lastErr, v.errAt = err.Error(), time.Now()
		return 0, err
	}
	if v.lastErr != "" {
		fmt.Fprintf(v.opts.Errors, "logging: storing logs in VictoriaLogs at %s again\n", target.URL)
		v.lastErr, v.errAt = "", time.Time{}
	}
	// Records may have been dropped from the front while posting
	sent := len(batch)
	if done := first + uint64(sent); done > v.head {
		n := min(int(done-v.head), len(v.queue))
		v.queue = v.queue[n:]
		v.head += uint64(n)
	}
	return sent, nil
}

func (v *Victoria) post(target Target, batch [][]byte) error {
	body := bytes.Join(batch, nil)
	url := target.URL + "/insert/jsonline?_stream_fields=" + StreamFields + "&_msg_field=_msg&_time_field=_time"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	// Without it VictoriaLogs reads the body as a form and stores nothing
	req.Header.Set("Content-Type", "application/stream+json")
	resp, err := target.Do(v.opts.Client, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
