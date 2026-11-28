// Package msgtest is the Messaging test server (forge 26f-1): a builtin
// connector type, msgtest.chat, implementing chat.messaging@1 over
// workspaces it keeps in memory, scripted through its own contract
// msgtest.control@1: someone outside posting, editing, deleting and
// reacting, a new channel, failures, an expired cursor and dropped
// notifications, so bridges are built and tested without an outside
// service. Connections naming the same workspace share it, as two people
// in one Slack do.
//
// It takes the Calendar test server's form (question 28.6, decided for
// calendars on 2026-11-27): a Go builtin with a control contract rather
// than a scripted sample app, so every Foundation package tests against it
// without installing an app.
//
// Its state lives in the process that serves the type (a test server, not
// a provider): give it one adapter instance.
package msgtest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/messaging"
)

// The test server's type and control contract.
const (
	TypeID     = "msgtest.chat"
	ControlID  = "msgtest.control"
	ControlRef = "msgtest.control@1"
)

const settings = `{"type":"object","properties":{
  "workspace":{"type":"string","title":"Workspace","description":"The pretend workspace; connections naming the same one share its channels and messages. The connection's own when left empty"},
  "user":{"type":"string","title":"User id","description":"The account's member id, e.g. u-tony; u-<connection> when left empty"},
  "name":{"type":"string","title":"Name","description":"The account's display name"},
  "email":{"type":"string","title":"Email","description":"The account's email"}}}`

// Type is the test server's connector type.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{messaging.ContractRef, ControlRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(settings),
		Meta: connectors.Meta{Title: "Messaging test server", Description: "A pretend chat workspace in memory: implements chat.messaging@1 with #general and #random and two other members, Dana and Lee, and lets tests have someone post, edit, delete or react, add a channel, fail calls, expire the cursor and drop notifications through msgtest.control@1. Connections naming the same workspace share it. For tests and trying bridges by hand; it reaches no real chat service.", Tags: []string{"chat", "messaging", "test"}},
	}
}

// ControlContract is msgtest.control@1.
func ControlContract() connectors.Contract {
	ok := json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Always true"}},"required":["ok"]}`)
	idResult := json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The message's id"}},"required":["id"]}`)
	return connectors.Contract{
		ID: ControlID, Version: 1,
		Meta: connectors.Meta{Title: "Messaging test server control", Description: "Makes the Messaging test server act like the outside world: someone posting, editing, deleting or reacting in a channel, a channel being made, calls failing, the cursor expiring and notifications getting lost; and shows its channels, messages and pending faults. For tests only.", Tags: []string{"chat", "messaging", "test"}},
		Operations: []connectors.Operation{
			{
				ID: "post", Title: "Someone posts", Description: "Posts a message as another member (Dana, Lee or anyone named), as if they wrote it at the service, raising messagePosted.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "channel":{"type":"string","description":"The channel's id; general when left out"},
  "user":{"type":"string","description":"The member's id; u-dana when left out (an unknown id joins the workspace)"},
  "text":{"type":"string","description":"The text"},
  "thread":{"type":"string","description":"Reply in this message's thread"}},"required":["text"]}`),
				Result:   idResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general","user":"u-dana","text":"Standup moved to 10:30"}`), Result: json.RawMessage(`{"id":"m-1"}`)}},
				Errors:   []string{connectors.CodeNotFound},
			},
			{
				ID: "edit", Title: "Someone edits", Description: "Changes any message's text as if its author edited it at the service, raising messageEdited.",
				Effect:   connectors.EffectWrite,
				Args:     json.RawMessage(`{"type":"object","properties":{"channel":{"type":"string","description":"The channel's id; general when left out"},"id":{"type":"string","description":"The message's id"},"text":{"type":"string","description":"The new text"}},"required":["id","text"]}`),
				Result:   idResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"m-1","text":"Standup moved to 10:45"}`), Result: json.RawMessage(`{"id":"m-1"}`)}},
				Errors:   []string{connectors.CodeNotFound},
			},
			{
				ID: "delete", Title: "Someone deletes", Description: "Deletes any message as if its author did at the service, raising messageDeleted.",
				Effect: connectors.EffectWrite, Destructive: true,
				Args:     json.RawMessage(`{"type":"object","properties":{"channel":{"type":"string","description":"The channel's id; general when left out"},"id":{"type":"string","description":"The message's id"}},"required":["id"]}`),
				Result:   ok,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"m-1"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   []string{connectors.CodeNotFound},
			},
			{
				ID: "react", Title: "Someone reacts", Description: "Adds or takes back a member's reaction, raising reactionChanged.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "channel":{"type":"string","description":"The channel's id; general when left out"},
  "id":{"type":"string","description":"The message's id"},
  "user":{"type":"string","description":"The member's id; u-lee when left out"},
  "emoji":{"type":"string","description":"The emoji's name"},
  "removed":{"type":"boolean","description":"Take the reaction back instead"}},"required":["id","emoji"]}`),
				Result:   ok,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"m-1","user":"u-lee","emoji":"thumbsup"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   []string{connectors.CodeNotFound},
			},
			{
				ID: "addChannel", Title: "Make a channel", Description: "Makes a channel at the service, raising channelChanged. Every member of the workspace joins it unless members are given.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "name":{"type":"string","description":"Its name"},
  "kind":{"type":"string","enum":["channel","private","direct","group"],"description":"Its kind; channel when left out"},
  "members":{"type":"array","items":{"type":"string"},"description":"Its members' ids"}},"required":["name"]}`),
				Result:   json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The channel's id"}},"required":["id"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"name":"launch"}`), Result: json.RawMessage(`{"id":"c-launch"}`)}},
			},
			{
				ID: "fail", Title: "Fail the next calls", Description: "Makes the next times calls of a chat.messaging operation fail with the error kind given (rateLimited and unavailable are retried by jobs).",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "operation":{"type":"string","description":"The chat.messaging operation, e.g. message.post"},
  "times":{"type":"integer","minimum":1,"description":"How many calls fail; once when left out"},
  "kind":{"type":"string","enum":["auth","notFound","conflict","invalid","rateLimited","unavailable","notSupported"],"description":"The error code; unavailable when left out"}},"required":["operation"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"operation":"message.post","times":2,"kind":"rateLimited"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors: []string{connectors.CodeInvalid},
			},
			{
				ID: "expireCursor", Title: "Expire the cursor", Description: "Makes every cursor given out so far too old, so the next messages.list with one is refused with cursor_expired.",
				Effect: connectors.EffectWrite, Args: json.RawMessage(`{"type":"object","properties":{}}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true}`)}},
			},
			{
				ID: "dropNotifications", Title: "Drop notifications", Description: "Loses the next n notices as a flaky push would, while the changes themselves still happen, to prove a missed notice is caught up by the next list.",
				Effect: connectors.EffectWrite,
				Args:   json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","minimum":1,"description":"How many notices to lose"}},"required":["n"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"n":1}`), Result: json.RawMessage(`{"ok":true}`)}},
			},
			{
				ID: "state", Title: "Show the state", Description: "The workspace's channels with their messages, its members, the cursor and the faults still to happen.",
				Effect: connectors.EffectRead, Args: json.RawMessage(`{"type":"object","properties":{}}`),
				Result: json.RawMessage(`{"type":"object","properties":{
  "channels":{"type":"array","description":"The channels with their messages","items":{"type":"object","properties":{"id":{"type":"string","description":"Its id"},"name":{"type":"string","description":"Its name"},"messages":{"type":"array","items":` + messaging.MessageSchema + `,"description":"Its messages, replies included, oldest first"}},"required":["id","messages"]}},
  "members":{"type":"array","items":` + messaging.MemberSchema + `,"description":"The workspace's members"},
  "cursor":{"type":"string","description":"The current cursor"},
  "faults":{"type":"array","description":"Faults still to happen","items":{"type":"object","properties":{"operation":{"type":"string","description":"The operation"},"times":{"type":"integer","description":"Calls left to fail"},"kind":{"type":"string","description":"The error code"}},"required":["operation"]}},
  "drop":{"type":"integer","description":"Notices still to lose"}},"required":["channels","members","faults"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"channels":[{"id":"general","name":"general","messages":[]}],"members":[{"id":"u-dana","name":"Dana"}],"cursor":"0","faults":[],"drop":0}`)}},
			},
		},
	}
}

// Contribute registers the test server's type and control contract.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, TypeID, []connectors.Contract{ControlContract()}, []connectors.Type{Type()})
}

// Server holds every workspace in this process.
type Server struct {
	mu     sync.Mutex
	spaces map[string]*space
	// Now is the clock messages are stamped with.
	Now func() time.Time
}

// New makes an empty server.
func New() *Server {
	return &Server{spaces: map[string]*space{}, Now: func() time.Time { return time.Now().UTC() }}
}

// Default is this process's server.
var Default = New()

type channel struct {
	messaging.Channel
	members []string
}

type stored struct {
	msg  messaging.Message
	gone bool
}

type change struct {
	seq     int
	kind    string // the event's name
	channel string
	message string
	emoji   string
	user    string
	added   bool
	notice  bool
}

type fault struct {
	times int
	kind  string
}

type space struct {
	channels      []*channel
	members       map[string]messaging.Member
	messages      map[string]*stored
	order         []string          // message ids in posting order
	keys          map[string]string // post key → message id
	files         map[string]messaging.File
	changes       []change
	expiredBefore int
	drop          int
	faults        map[string]*fault
	next          int
	wake          chan struct{}
}

func (s *Server) space(tenant uint64, name string) *space {
	k := strconv.FormatUint(tenant, 10) + "." + name
	sp := s.spaces[k]
	if sp == nil {
		sp = &space{members: map[string]messaging.Member{
			"u-dana": {ID: "u-dana", Name: "Dana", Email: "dana@example.com"},
			"u-lee":  {ID: "u-lee", Name: "Lee", Email: "lee@example.com"},
		}, messages: map[string]*stored{}, keys: map[string]string{}, files: map[string]messaging.File{}, faults: map[string]*fault{}, wake: make(chan struct{})}
		sp.channels = []*channel{
			{Channel: messaging.Channel{ID: "general", Name: "general", Kind: messaging.KindChannel, Topic: "Everyone"}, members: []string{"u-dana", "u-lee"}},
			{Channel: messaging.Channel{ID: "random", Name: "random", Kind: messaging.KindChannel}, members: []string{"u-dana", "u-lee"}},
		}
		s.spaces[k] = sp
	}
	return sp
}

// join adds a member to the workspace and its public channels.
func (sp *space) join(m messaging.Member) {
	if _, ok := sp.members[m.ID]; ok {
		if m.Name != "" {
			old := sp.members[m.ID]
			old.Name = m.Name
			if m.Email != "" {
				old.Email = m.Email
			}
			sp.members[m.ID] = old
		}
		return
	}
	if m.Name == "" {
		m.Name = strings.TrimPrefix(m.ID, "u-")
	}
	sp.members[m.ID] = m
	for _, c := range sp.channels {
		if c.Kind == messaging.KindChannel && !contains(c.members, m.ID) {
			c.members = append(c.members, m.ID)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func (sp *space) channel(id string) *channel {
	for _, c := range sp.channels {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (sp *space) cursor() int { return len(sp.changes) }

// log records a change and wakes the watches; under s.mu.
func (sp *space) log(c change) {
	c.notice = true
	if sp.drop > 0 {
		sp.drop--
		c.notice = false
	}
	c.seq = len(sp.changes) + 1
	sp.changes = append(sp.changes, c)
	close(sp.wake)
	sp.wake = make(chan struct{})
}

var mention = regexp.MustCompile(`@(u-[A-Za-z0-9_-]+)`)

func (sp *space) mentions(text string) []string {
	var out []string
	for _, m := range mention.FindAllStringSubmatch(text, -1) {
		if _, ok := sp.members[m[1]]; ok && !contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

func (sp *space) message(ch, id string) *stored {
	st := sp.messages[id]
	if st == nil || st.gone || st.msg.Channel != ch {
		return nil
	}
	return st
}

// post makes a message; under s.mu.
func (s *Server) post(sp *space, ch *channel, author messaging.Member, text, thread, key string, files []messaging.File) (messaging.Message, error) {
	if thread != "" {
		parent := sp.message(ch.ID, thread)
		if parent == nil {
			return messaging.Message{}, errf(connectors.CodeNotFound, "there is no message %q to reply to", thread)
		}
		if parent.msg.Thread != "" {
			thread = parent.msg.Thread // a reply to a reply joins its thread
			parent = sp.messages[thread]
		}
		parent.msg.Replies++
	}
	sp.next++
	m := messaging.Message{
		ID: fmt.Sprintf("m-%d", sp.next), Channel: ch.ID, Thread: thread, Key: key,
		Author: messaging.Author{ID: author.ID, Name: author.Name, Bot: author.Bot},
		Text:   text, Mentions: sp.mentions(text), Files: files, Posted: s.Now().Format(time.RFC3339),
	}
	sp.messages[m.ID] = &stored{msg: m}
	sp.order = append(sp.order, m.ID)
	if key != "" {
		sp.keys[key] = m.ID
	}
	sp.log(change{kind: "messagePosted", channel: ch.ID, message: m.ID})
	return m, nil
}

func (s *Server) edit(sp *space, st *stored, text string) messaging.Message {
	st.msg.Text, st.msg.Mentions, st.msg.Edited = text, sp.mentions(text), s.Now().Format(time.RFC3339)
	sp.log(change{kind: "messageEdited", channel: st.msg.Channel, message: st.msg.ID})
	return st.msg
}

func (sp *space) remove(st *stored) {
	st.gone = true
	if t := st.msg.Thread; t != "" {
		if p := sp.messages[t]; p != nil && p.msg.Replies > 0 {
			p.msg.Replies--
		}
	}
	sp.log(change{kind: "messageDeleted", channel: st.msg.Channel, message: st.msg.ID})
}

// react adds or takes back a reaction; it reports whether anything changed.
func (sp *space) react(st *stored, user, emoji string, add bool) bool {
	rs := st.msg.Reactions
	i := -1
	for j, r := range rs {
		if r.Emoji == emoji {
			i = j
		}
	}
	switch {
	case add && i >= 0 && contains(rs[i].Users, user), !add && (i < 0 || !contains(rs[i].Users, user)):
		return false
	case add && i >= 0:
		rs[i].Users = append(rs[i].Users, user)
		rs[i].Count = len(rs[i].Users)
	case add:
		rs = append(rs, messaging.Reaction{Emoji: emoji, Count: 1, Users: []string{user}})
	default:
		var users []string
		for _, u := range rs[i].Users {
			if u != user {
				users = append(users, u)
			}
		}
		if len(users) == 0 {
			rs = append(rs[:i], rs[i+1:]...)
		} else {
			rs[i].Users, rs[i].Count = users, len(users)
		}
	}
	st.msg.Reactions = rs
	sp.log(change{kind: "reactionChanged", channel: st.msg.Channel, message: st.msg.ID, emoji: emoji, user: user, added: add})
	return true
}

// Impl is the adapter implementation of the test server's type.
func (s *Server) Impl() adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st struct{ Workspace, User, Name, Email string }
		_ = json.Unmarshal(conn.Settings, &st)
		if st.Workspace == "" {
			st.Workspace = conn.ID
		}
		if st.User == "" {
			st.User = "u-" + conn.ID
		}
		if st.Name == "" {
			st.Name = "Test user"
		}
		s.mu.Lock()
		s.space(ctx.Tenant(), st.Workspace).join(messaging.Member{ID: st.User, Name: st.Name, Email: st.Email})
		s.mu.Unlock()
		return &session{s: s, tenant: ctx.Tenant(), conn: conn.ID, workspace: st.Workspace, user: st.User}, nil
	}}
}

// Options are adapter options serving only the test server, for tests.
func (s *Server) Options(name string) adapter.Options {
	return adapter.Options{Name: name, Types: []connectors.Type{Type()}, Contracts: []connectors.Contract{messaging.Contract(), ControlContract()}, Impls: []adapter.Impl{s.Impl()}}
}

type session struct {
	s         *Server
	tenant    uint64
	conn      string
	workspace string
	user      string
}

func (x *session) Close() error { return nil }

// Watch raises the contract's events for each change after the cursor in
// the workspace (notices dropNotifications lost are skipped, their changes
// kept for messages.list).
func (x *session) Watch(ctx adapter.Context, cursor json.RawMessage, emit func(adapter.Event) error) error {
	from := 0
	if len(cursor) > 0 {
		_ = json.Unmarshal(cursor, &from)
	}
	for {
		x.s.mu.Lock()
		sp := x.s.space(x.tenant, x.workspace)
		pending := append([]change(nil), sp.changes[min(from, len(sp.changes)):]...)
		payloads := make([][]byte, len(pending))
		for i, c := range pending {
			if !c.notice {
				continue
			}
			p := map[string]any{"id": fmt.Sprintf("%s-%d", x.conn, c.seq), "channel": c.channel}
			switch c.kind {
			case "messagePosted", "messageEdited":
				p["message"] = sp.messages[c.message].msg
			case "messageDeleted":
				p["message"] = c.message
			case "reactionChanged":
				p["message"], p["emoji"], p["user"], p["added"] = c.message, c.emoji, c.user, c.added
			}
			payloads[i], _ = json.Marshal(p)
		}
		wake := sp.wake
		x.s.mu.Unlock()
		for i, c := range pending {
			from = c.seq
			if !c.notice {
				continue
			}
			cur, _ := json.Marshal(c.seq)
			if err := emit(adapter.Event{Contract: messaging.ContractRef, Name: c.kind, ID: fmt.Sprintf("%s-%d", x.conn, c.seq), Payload: payloads[i], Cursor: cur}); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func errf(code, format string, args ...any) error { return connectors.Errorf(code, format, args...) }

func (x *session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	sp := x.s.space(x.tenant, x.workspace)
	if ctx.Contract == ControlRef {
		return x.control(sp, op, args)
	}
	if f := sp.faults[op]; f != nil && f.times > 0 {
		f.times--
		if f.times == 0 {
			delete(sp.faults, op)
		}
		return nil, errf(f.kind, "the test server failed %s as asked", op)
	}
	var in struct {
		Channel     string   `json:"channel"`
		ID          string   `json:"id"`
		Key         string   `json:"key"`
		Text        string   `json:"text"`
		Thread      string   `json:"thread"`
		Since       string   `json:"since"`
		Limit       int      `json:"limit"`
		Files       []string `json:"files"`
		Emoji       string   `json:"emoji"`
		Name        string   `json:"name"`
		ContentType string   `json:"contentType"`
		Data        string   `json:"data"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, errf(connectors.CodeInvalid, "the arguments cannot be read: %v", err)
	}
	switch op {
	case "check":
		return json.RawMessage(`{"ok":true}`), nil
	case "channels.list":
		var out []messaging.Channel
		for _, c := range sp.channels {
			if c.Kind == messaging.KindChannel || contains(c.members, x.user) {
				ch := c.Channel
				ch.Members = len(c.members)
				out = append(out, ch)
			}
		}
		return json.Marshal(map[string]any{"channels": out})
	case "members.list":
		ids := make([]string, 0, len(sp.members))
		if in.Channel != "" {
			c := x.visible(sp, in.Channel)
			if c == nil {
				return nil, errf(connectors.CodeNotFound, "there is no channel %q", in.Channel)
			}
			ids = append(ids, c.members...)
		} else {
			for id := range sp.members {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		out := []messaging.Member{}
		for _, id := range ids {
			out = append(out, sp.members[id])
		}
		return json.Marshal(map[string]any{"members": out})
	}
	c := x.visible(sp, in.Channel)
	if c == nil {
		return nil, errf(connectors.CodeNotFound, "there is no channel %q", in.Channel)
	}
	switch op {
	case "messages.list":
		return sp.list(c.ID, in.Thread, in.Since, in.Limit)
	case "message.post":
		if in.Key == "" || strings.TrimSpace(in.Text) == "" && len(in.Files) == 0 {
			return nil, errf(connectors.CodeInvalid, "give the message's key and its text")
		}
		if c.Archived {
			return nil, errf(connectors.CodeInvalid, "channel %q is archived", c.ID)
		}
		if id, ok := sp.keys[in.Key]; ok {
			if st := sp.messages[id]; st != nil {
				return json.Marshal(st.msg)
			}
		}
		var files []messaging.File
		for _, id := range in.Files {
			f, ok := sp.files[id]
			if !ok {
				return nil, errf(connectors.CodeInvalid, "there is no uploaded file %q", id)
			}
			files = append(files, f)
		}
		m, err := x.s.post(sp, c, sp.members[x.user], in.Text, in.Thread, in.Key, files)
		if err != nil {
			return nil, err
		}
		return json.Marshal(m)
	case "message.edit":
		st := sp.message(c.ID, in.ID)
		if st == nil {
			return nil, errf(connectors.CodeNotFound, "there is no message %q", in.ID)
		}
		if st.msg.Author.ID != x.user {
			return nil, errf(connectors.CodeInvalid, "only its author may edit message %q", in.ID)
		}
		return json.Marshal(x.s.edit(sp, st, in.Text))
	case "message.delete":
		st := sp.message(c.ID, in.ID)
		if st == nil {
			return json.RawMessage(`{"deleted":false}`), nil
		}
		if st.msg.Author.ID != x.user {
			return nil, errf(connectors.CodeInvalid, "only its author may delete message %q", in.ID)
		}
		sp.remove(st)
		return json.RawMessage(`{"deleted":true}`), nil
	case "reaction.add", "reaction.remove":
		st := sp.message(c.ID, in.ID)
		if st == nil {
			return nil, errf(connectors.CodeNotFound, "there is no message %q", in.ID)
		}
		if in.Emoji == "" {
			return nil, errf(connectors.CodeInvalid, "give the emoji")
		}
		sp.react(st, x.user, in.Emoji, op == "reaction.add")
		return json.RawMessage(`{"ok":true}`), nil
	case "file.upload":
		data, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil || in.Name == "" {
			return nil, errf(connectors.CodeInvalid, "give the file's name and its content base64 encoded")
		}
		sp.next++
		f := messaging.File{ID: fmt.Sprintf("f-%d", sp.next), Name: in.Name, Size: int64(len(data)), ContentType: in.ContentType}
		sp.files[f.ID] = f
		return json.Marshal(f)
	}
	return nil, errf(connectors.CodeNotSupported, "the test server has no operation %q", op)
}

// visible is a channel the session's account may read.
func (x *session) visible(sp *space, id string) *channel {
	c := sp.channel(id)
	if c == nil || c.Kind != messaging.KindChannel && !contains(c.members, x.user) {
		return nil
	}
	return c
}

func (sp *space) list(ch, thread, since string, limit int) (json.RawMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	keep := func(m messaging.Message) bool {
		if thread != "" {
			return m.Thread == thread
		}
		return m.Thread == ""
	}
	out := struct {
		Messages []messaging.Message `json:"messages"`
		Deleted  []string            `json:"deleted,omitempty"`
		Cursor   string              `json:"cursor"`
	}{Messages: []messaging.Message{}, Cursor: strconv.Itoa(sp.cursor())}
	if since != "" {
		from, err := strconv.Atoi(since)
		if err != nil || from < 0 || from > sp.cursor() {
			return nil, errf(connectors.CodeInvalid, "cursor %q is not one this server gave", since)
		}
		if from < sp.expiredBefore {
			return nil, errf(connectors.CodeInvalid, "%s: the cursor is too old, list again without one", messaging.CodeCursorExpired)
		}
		seen := map[string]bool{}
		var changed []string
		for _, c := range sp.changes[from:] {
			if c.channel != ch || c.message == "" || seen[c.message] {
				continue
			}
			seen[c.message] = true
			changed = append(changed, c.message)
		}
		// oldest first, by posting order
		sort.SliceStable(changed, func(i, j int) bool { return sp.index(changed[i]) < sp.index(changed[j]) })
		for _, id := range changed {
			st := sp.messages[id]
			if !keep(st.msg) {
				continue
			}
			if st.gone {
				out.Deleted = append(out.Deleted, id)
			} else if len(out.Messages) < limit {
				out.Messages = append(out.Messages, st.msg)
			}
		}
		return json.Marshal(out)
	}
	var all []messaging.Message
	for _, id := range sp.order {
		st := sp.messages[id]
		if st.gone || st.msg.Channel != ch || !keep(st.msg) {
			continue
		}
		all = append(all, st.msg)
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	out.Messages = append(out.Messages, all...)
	return json.Marshal(out)
}

func (sp *space) index(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "m-"))
	return n
}

func (x *session) control(sp *space, op string, args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Channel   string   `json:"channel"`
		ID        string   `json:"id"`
		User      string   `json:"user"`
		Text      string   `json:"text"`
		Thread    string   `json:"thread"`
		Emoji     string   `json:"emoji"`
		Removed   bool     `json:"removed"`
		Name      string   `json:"name"`
		Kind      string   `json:"kind"`
		Members   []string `json:"members"`
		Operation string   `json:"operation"`
		Times     int      `json:"times"`
		N         int      `json:"n"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, errf(connectors.CodeInvalid, "the arguments cannot be read: %v", err)
	}
	if in.Channel == "" {
		in.Channel = "general"
	}
	idOf := func(id string) (json.RawMessage, error) { return json.Marshal(map[string]string{"id": id}) }
	switch op {
	case "post":
		c := sp.channel(in.Channel)
		if c == nil {
			return nil, errf(connectors.CodeNotFound, "there is no channel %q", in.Channel)
		}
		if in.User == "" {
			in.User = "u-dana"
		}
		sp.join(messaging.Member{ID: in.User})
		if !contains(c.members, in.User) {
			c.members = append(c.members, in.User)
		}
		m, err := x.s.post(sp, c, sp.members[in.User], in.Text, in.Thread, "", nil)
		if err != nil {
			return nil, err
		}
		return idOf(m.ID)
	case "edit":
		st := sp.message(in.Channel, in.ID)
		if st == nil {
			return nil, errf(connectors.CodeNotFound, "there is no message %q", in.ID)
		}
		return idOf(x.s.edit(sp, st, in.Text).ID)
	case "delete":
		st := sp.message(in.Channel, in.ID)
		if st == nil {
			return nil, errf(connectors.CodeNotFound, "there is no message %q", in.ID)
		}
		sp.remove(st)
		return json.RawMessage(`{"ok":true}`), nil
	case "react":
		st := sp.message(in.Channel, in.ID)
		if st == nil {
			return nil, errf(connectors.CodeNotFound, "there is no message %q", in.ID)
		}
		if in.User == "" {
			in.User = "u-lee"
		}
		sp.join(messaging.Member{ID: in.User})
		sp.react(st, in.User, in.Emoji, !in.Removed)
		return json.RawMessage(`{"ok":true}`), nil
	case "addChannel":
		if in.Kind == "" {
			in.Kind = messaging.KindChannel
		}
		id := "c-" + strings.ToLower(strings.Join(strings.Fields(in.Name), "-"))
		if sp.channel(id) != nil {
			sp.next++
			id = fmt.Sprintf("%s-%d", id, sp.next)
		}
		members := in.Members
		if len(members) == 0 {
			for m := range sp.members {
				members = append(members, m)
			}
			sort.Strings(members)
		}
		sp.channels = append(sp.channels, &channel{Channel: messaging.Channel{ID: id, Name: in.Name, Kind: in.Kind}, members: members})
		sp.log(change{kind: "channelChanged", channel: id})
		return idOf(id)
	case "fail":
		if in.Operation == "" {
			return nil, errf(connectors.CodeInvalid, "name the operation to fail")
		}
		if in.Times <= 0 {
			in.Times = 1
		}
		if in.Kind == "" {
			in.Kind = connectors.CodeUnavailable
		}
		sp.faults[in.Operation] = &fault{times: in.Times, kind: in.Kind}
		return json.RawMessage(`{"ok":true}`), nil
	case "expireCursor":
		sp.expiredBefore = sp.cursor() + 1
		return json.RawMessage(`{"ok":true}`), nil
	case "dropNotifications":
		sp.drop += max(in.N, 1)
		return json.RawMessage(`{"ok":true}`), nil
	case "state":
		type chState struct {
			ID       string              `json:"id"`
			Name     string              `json:"name"`
			Messages []messaging.Message `json:"messages"`
		}
		type faultState struct {
			Operation string `json:"operation"`
			Times     int    `json:"times"`
			Kind      string `json:"kind"`
		}
		out := struct {
			Channels []chState          `json:"channels"`
			Members  []messaging.Member `json:"members"`
			Cursor   string             `json:"cursor"`
			Faults   []faultState       `json:"faults"`
			Drop     int                `json:"drop"`
		}{Cursor: strconv.Itoa(sp.cursor()), Faults: []faultState{}, Drop: sp.drop}
		for _, c := range sp.channels {
			cs := chState{ID: c.ID, Name: c.Name, Messages: []messaging.Message{}}
			for _, id := range sp.order {
				if st := sp.messages[id]; !st.gone && st.msg.Channel == c.ID {
					cs.Messages = append(cs.Messages, st.msg)
				}
			}
			out.Channels = append(out.Channels, cs)
		}
		ids := make([]string, 0, len(sp.members))
		for id := range sp.members {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			out.Members = append(out.Members, sp.members[id])
		}
		ops := make([]string, 0, len(sp.faults))
		for o := range sp.faults {
			ops = append(ops, o)
		}
		sort.Strings(ops)
		for _, o := range ops {
			out.Faults = append(out.Faults, faultState{Operation: o, Times: sp.faults[o].times, Kind: sp.faults[o].kind})
		}
		return json.Marshal(out)
	}
	return nil, errf(connectors.CodeNotSupported, "msgtest.control has no operation %q", op)
}
