// Package liveclient talks to a running serve from Go outside it (Forge's
// developer tools since forge 7p-1, the seed's draft since forge 22o): REST
// under /api/v1 and the live connection (/api/v1/live),
// signed in as a user (or as user 1 on a single-user server, with no
// sign-in). It sends commands, takes one snapshot of a query and runs
// calls, as the browser does.
package liveclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Client is one signed-in connection to a server. Its methods may be
// called from several goroutines.
type Client struct {
	Server   string // e.g. http://localhost:8080
	Username string // empty on a single-user server
	Password string

	http     *http.Client
	mu       sync.Mutex
	signedIn bool
	conn     *websocket.Conn
	waiting  map[string]chan frame // by command, subscription or call id
	n        atomic.Int64
	run      string
}

// New makes a client; nothing is sent until it is used.
func New(server, username, password string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		Server: strings.TrimSuffix(server, "/"), Username: username, Password: password,
		http:    &http.Client{Jar: jar, Timeout: 5 * time.Minute},
		waiting: map[string]chan frame{},
		run:     fmt.Sprintf("%x", time.Now().UnixNano()),
	}
}

// NewID is an id unique across runs, for commands (a reused command id is
// acked as a duplicate for 10 minutes and commits nothing).
func (c *Client) NewID(prefix string) string {
	return fmt.Sprintf("%s-%s-%d", prefix, c.run, c.n.Add(1))
}

// HTTP is the client's HTTP client, holding its session cookies.
func (c *Client) HTTP() *http.Client { return c.http }

// SignIn signs in once, when a username is set.
func (c *Client) SignIn(ctx context.Context) error {
	c.mu.Lock()
	done := c.signedIn
	c.mu.Unlock()
	if done || c.Username == "" {
		return nil
	}
	if _, err := c.post(ctx, "/identity/login", map[string]string{"username": c.Username, "password": c.Password}); err != nil {
		return fmt.Errorf("signing in as %s: %w", c.Username, err)
	}
	c.mu.Lock()
	c.signedIn = true
	c.mu.Unlock()
	return nil
}

// Post sends JSON to /api/v1<path> and gives the response body, or the
// server's message as the error.
func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	if err := c.SignIn(ctx); err != nil {
		return nil, err
	}
	return c.post(ctx, path, body)
}

// Get reads /api/v1<path>.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	if err := c.SignIn(ctx); err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.Server+"/api/v1"+path, nil)
	return c.do(req)
}

func (c *Client) post(ctx context.Context, path string, body any) ([]byte, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Server+"/api/v1"+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w (is forge serve running at %s?)", err, c.Server)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var status struct{ Message, ErrorCode string }
		if json.Unmarshal(out, &status) == nil && status.Message != "" {
			return nil, fmt.Errorf("%s (%d %s)", status.Message, resp.StatusCode, status.ErrorCode)
		}
		return nil, fmt.Errorf("%s %s: %s", req.Method, req.URL.Path, resp.Status)
	}
	return out, nil
}

// frame is any frame the server sends.
type frame struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Sub       string          `json:"sub,omitempty"`
	Code      int             `json:"code,omitempty"`
	Message   string          `json:"message,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
	Problems  json.RawMessage `json:"problems,omitempty"`
	Target    string          `json:"target,omitempty"`
	Seq       uint64          `json:"seq,omitempty"`
	Duplicate bool            `json:"duplicate,omitempty"`
	Request   string          `json:"request,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Event     string          `json:"event,omitempty"`
	Delta     json.RawMessage `json:"delta,omitempty"`
}

// live opens the live connection once.
func (c *Client) live(ctx context.Context) (*websocket.Conn, error) {
	if err := c.SignIn(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn, nil
	}
	url := "ws" + strings.TrimPrefix(c.Server, "http") + "/api/v1/live"
	dial, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dial, url, &websocket.DialOptions{HTTPClient: &http.Client{Jar: c.http.Jar}})
	if err != nil {
		return nil, fmt.Errorf("opening the live connection: %w (is forge serve running at %s?)", err, c.Server)
	}
	conn.SetReadLimit(64 << 20)
	c.conn = conn
	go c.read(conn)
	return conn, nil
}

func (c *Client) read(conn *websocket.Conn) {
	for {
		typ, msg, err := conn.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			if c.conn == conn {
				c.conn = nil
			}
			for id, ch := range c.waiting {
				select {
				case ch <- frame{Type: "closed", Message: err.Error()}:
				default:
				}
				delete(c.waiting, id)
			}
			c.mu.Unlock()
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var f frame
		if json.Unmarshal(msg, &f) != nil {
			continue
		}
		key := f.ID
		if f.Sub != "" {
			key = f.Sub
		}
		c.mu.Lock()
		ch := c.waiting[key]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- f:
			default: // a slow reader misses changes, never snapshots it waits for
			}
		}
	}
}

func (c *Client) send(ctx context.Context, v any) error {
	conn, err := c.live(ctx)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(v)
	return conn.Write(ctx, websocket.MessageText, raw)
}

func (c *Client) wait(id string) chan frame {
	ch := make(chan frame, 64)
	c.mu.Lock()
	c.waiting[id] = ch
	c.mu.Unlock()
	return ch
}

func (c *Client) done(id string) {
	c.mu.Lock()
	delete(c.waiting, id)
	c.mu.Unlock()
}

// Command is a command to send on the live connection.
type Command struct {
	ID      string          `json:"id"` // made when empty
	Command string          `json:"command"`
	Target  string          `json:"target"`
	Args    json.RawMessage `json:"args,omitempty"`
	Request string          `json:"request,omitempty"`
	App     string          `json:"app,omitempty"`
	Actor   string          `json:"actor,omitempty"`
	Branch  string          `json:"branch,omitempty"` // the branch of App it changes (forge 19a)
}

// Reply is a command's ack or rejection.
type Reply struct {
	OK        bool            `json:"ok"`
	ID        string          `json:"id"`
	Target    string          `json:"target,omitempty"` // the target made, when the command made one
	Seq       uint64          `json:"seq,omitempty"`
	Duplicate bool            `json:"duplicate,omitempty"`
	Request   string          `json:"request,omitempty"`
	Code      int             `json:"code,omitempty"`
	Message   string          `json:"message,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
	Problems  json.RawMessage `json:"problems,omitempty"`
}

// Send sends one command and waits for its ack or rejection; a rejection
// is a Reply with OK false, not an error.
func (c *Client) Send(ctx context.Context, cmd Command) (Reply, error) {
	if cmd.ID == "" {
		cmd.ID = c.NewID("forge")
	}
	ch := c.wait(cmd.ID)
	defer c.done(cmd.ID)
	if err := c.send(ctx, struct {
		Type string `json:"type"`
		Command
	}{"command", cmd}); err != nil {
		return Reply{}, err
	}
	select {
	case f := <-ch:
		switch f.Type {
		case "ack":
			return Reply{OK: true, ID: cmd.ID, Target: f.Target, Seq: f.Seq, Duplicate: f.Duplicate, Request: f.Request}, nil
		case "rejected":
			return Reply{ID: cmd.ID, Code: f.Code, Message: f.Message, ErrorCode: f.ErrorCode, Problems: f.Problems}, nil
		}
		return Reply{}, fmt.Errorf("the live connection closed: %s", f.Message)
	case <-ctx.Done():
		return Reply{}, ctx.Err()
	}
}

// Snapshot opens a query, gives its first snapshot's data and closes it.
func (c *Client) Snapshot(ctx context.Context, query string, params json.RawMessage) (json.RawMessage, error) {
	sub := c.NewID("sub")
	ch := c.wait(sub)
	defer c.done(sub)
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	if err := c.send(ctx, map[string]any{"type": "subscribe", "sub": sub, "query": query, "params": params}); err != nil {
		return nil, err
	}
	defer func() { _ = c.send(context.Background(), map[string]any{"type": "unsubscribe", "sub": sub}) }()
	for {
		select {
		case f := <-ch:
			switch f.Type {
			case "snapshot":
				return f.Data, nil
			case "subError":
				return nil, fmt.Errorf("%s: %s (%d %s)", query, f.Message, f.Code, f.ErrorCode)
			case "closed":
				return nil, fmt.Errorf("the live connection closed: %s", f.Message)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Event is one event of a call.
type Event struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Call runs a live call to its end and gives its events.
func (c *Client) Call(ctx context.Context, call string, args json.RawMessage) ([]Event, error) {
	id := c.NewID("call")
	ch := c.wait(id)
	defer c.done(id)
	if err := c.send(ctx, map[string]any{"type": "call", "id": id, "call": call, "args": args}); err != nil {
		return nil, err
	}
	var out []Event
	for {
		select {
		case f := <-ch:
			switch f.Type {
			case "event":
				out = append(out, Event{Event: f.Event, Data: f.Data})
			case "end":
				if f.Code != 0 && f.Code != 200 {
					return out, fmt.Errorf("%s: %s (%d %s)", call, f.Message, f.Code, f.ErrorCode)
				}
				return out, nil
			case "closed":
				return out, fmt.Errorf("the live connection closed: %s", f.Message)
			}
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
}

// Close closes the live connection.
func (c *Client) Close() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
}

// Rows is a query's data as a list of rows: a list as it is, an object's
// list under one of keys (records, jobs, items), or an object's values
// (a projection keyed by id), sorted by key.
func Rows(data json.RawMessage, keys ...string) []json.RawMessage {
	var list []json.RawMessage
	if json.Unmarshal(data, &list) == nil {
		return list
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil {
		return nil
	}
	for _, k := range append(keys, "records", "items", "rows") {
		if json.Unmarshal(obj[k], &list) == nil && list != nil {
			return list
		}
	}
	names := make([]string, 0, len(obj))
	for k := range obj {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		list = append(list, obj[k])
	}
	return list
}
