// Package trello is the tasks.trello connector type (forge 30i,
// documentation/Tasks.md › Built in forge 30i): tasks.tracker@1 on
// Trello's REST API with an API key and token, so a project syncs with a
// Trello board (30h).
//
// Trello has no idempotency keys, no card versions and no conditional
// writes, so the type makes them:
//   - a card's version is its dateLastActivity, which every action on it
//     moves; a write given a version reads the card first and refuses with
//     conflict when it moved on (a read and a write, not one atomic step);
//   - the key card.create and comment.create take is kept in the text as a
//     Markdown link reference definition ("[axiom-task]: #<key>"), which
//     Trello's Markdown does not show; a create looks for it before making
//     another, and card reads give it back as the card's key;
//   - the checklist is the card's first checklist ("Checklist"), its items
//     set by difference.
//
// Changes are polled: Watch reads each open board's actions since the
// last it saw (webhooks replace it once 26b's hook endpoint exists).
// Trello's rate limit (100 requests per 10 seconds per token) answers
// rateLimited with retryAfter.
package trello

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra/providers"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
)

// TypeID is the connector type.
const TypeID = "tasks.trello"

// API is Trello's REST API root.
const API = "https://api.trello.com"

// ChecklistName names the checklist a card's checklist items are kept in.
const ChecklistName = "Checklist"

// SettingsSchema is tasks.trello's settings.
const SettingsSchema = `{"type":"object","properties":{
  "key":{"type":"secret","title":"API key","description":"The Trello Power-Up's API key (trello.com/power-ups/admin)"},
  "token":{"type":"secret","title":"Token","description":"A token the board's member made for that key, with read and write scope"},
  "pollSeconds":{"type":"integer","title":"Poll every","description":"Seconds between reads of the boards' changes; 20 when left out","minimum":5,"maximum":3600}},
  "required":["key","token"]}`

// Type is tasks.trello.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{tracker.Ref}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Meta: connectors.Meta{Title: "Trello", Description: "Syncs tasks with Trello boards through Trello's REST API with an API key and token: implements tasks.tracker@1 with boards, lists, cards (with their members, labels, dates and first checklist) and comments, and polls the boards' actions for changes.", Tags: []string{"tasks", "tracker", "trello", "kanban"}},
	}
}

// Settings are a connection's settings, resolved.
type Settings struct {
	Key         string `json:"key"`
	Token       string `json:"token"`
	PollSeconds int    `json:"pollSeconds"`
}

// Options are for tests: Base replaces the API's root, HTTP the client,
// Poll the time between Watch's reads.
type Options struct {
	Base string
	HTTP *http.Client
	Poll time.Duration
}

// Impl is the adapter implementation.
func Impl(o Options) adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st Settings
		if len(conn.Settings) > 0 {
			if err := json.Unmarshal(conn.Settings, &st); err != nil {
				return nil, connectors.Errorf(connectors.CodeInvalid, "the connection's settings are not valid: %v", err)
			}
		}
		return New(st, o)
	}}
}

// Session is one connection's Trello account.
type Session struct {
	c    providers.Client
	poll time.Duration
	mu   sync.Mutex
	made map[string]string // keys this session made → card or comment id
}

// New opens a session from settings.
func New(st Settings, o Options) (*Session, error) {
	if st.Key == "" || st.Token == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the API key and token are required")
	}
	base := o.Base
	if base == "" {
		base = API
	}
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: providers.DefaultTimeout}
	}
	poll := o.Poll
	if poll == 0 && st.PollSeconds > 0 {
		poll = time.Duration(st.PollSeconds) * time.Second
	}
	if poll == 0 {
		poll = 20 * time.Second
	}
	auth := `OAuth oauth_consumer_key="` + st.Key + `", oauth_token="` + st.Token + `"`
	return &Session{poll: poll, made: map[string]string{}, c: providers.Client{Base: base, HTTP: hc, Message: message,
		Auth: func(r *http.Request) { r.Header.Set("Authorization", auth) }}}, nil
}

// message reads Trello's error: plain text, or JSON with a message.
func message(body []byte) string {
	var e struct{ Message, Error string }
	if json.Unmarshal(body, &e) == nil {
		if e.Message != "" {
			return e.Message
		}
		return e.Error
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Close closes nothing.
func (s *Session) Close() error { return nil }

// do calls Trello: Trello's "invalid id" (a 400 for an id of the wrong
// shape) is notFound, and a 429 says to wait out Trello's ten seconds.
func (s *Session) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	h, err := s.c.Do(ctx, method, "/1"+path, q, body, out)
	ce := connectors.AsError(err)
	if ce == nil {
		return err
	}
	if ce.Code == connectors.CodeInvalid && strings.Contains(ce.Message, "invalid id") {
		return connectors.Errorf(connectors.CodeNotFound, "%s", ce.Message)
	}
	if ce.Code == connectors.CodeRateLimited {
		ce.RetryAfterMs = 10_000
		if h != nil {
			if n, err := strconv.Atoi(h.Get("Retry-After")); err == nil && n > 0 {
				ce.RetryAfterMs = int64(n) * 1000
			}
		}
	}
	return ce
}

// Trello's shapes.
type tList struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Pos    float64 `json:"pos"`
	Closed bool    `json:"closed"`
}

type tLabel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type tMember struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Username string `json:"username"`
}

type tBoard struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	URL     string    `json:"url"`
	Closed  bool      `json:"closed"`
	Lists   []tList   `json:"lists"`
	Labels  []tLabel  `json:"labels"`
	Members []tMember `json:"members"`
}

type tCheckItem struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	State string  `json:"state"`
	Pos   float64 `json:"pos"`
}

type tChecklist struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	CheckItems []tCheckItem `json:"checkItems"`
}

type tCard struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Desc             string       `json:"desc"`
	IDBoard          string       `json:"idBoard"`
	IDList           string       `json:"idList"`
	IDMembers        []string     `json:"idMembers"`
	IDLabels         []string     `json:"idLabels"`
	Due              *string      `json:"due"`
	Start            *string      `json:"start"`
	Pos              float64      `json:"pos"`
	Closed           bool         `json:"closed"`
	DateLastActivity string       `json:"dateLastActivity"`
	ShortURL         string       `json:"shortUrl"`
	Checklists       []tChecklist `json:"checklists"`
}

type tAction struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Date            string `json:"date"`
	IDMemberCreator string `json:"idMemberCreator"`
	Data            struct {
		Text string                     `json:"text"`
		Card struct{ ID string }        `json:"card"`
		Old  map[string]json.RawMessage `json:"old"`
	} `json:"data"`
}

// The fields a card read asks for.
const cardFields = "closed,dateLastActivity,desc,due,idBoard,idLabels,idList,idMembers,name,pos,shortUrl,start"

func cardQuery() url.Values {
	return url.Values{"fields": {cardFields}, "checklists": {"all"}, "checklist_fields": {"name"}}
}

var (
	taskMarker    = regexp.MustCompile(`(?:\n*)\[axiom-task\]: #(\S+)\s*$`)
	commentMarker = regexp.MustCompile(`(?:\n*)\[axiom-comment\]: #(\S+)\s*$`)
)

// split gives a text without its marker, and the marker's key.
func split(re *regexp.Regexp, text string) (string, string) {
	m := re.FindStringSubmatchIndex(text)
	if m == nil {
		return text, ""
	}
	return text[:m[0]], text[m[2]:m[3]]
}

func withMarker(text, name, key string) string {
	if key == "" {
		return text
	}
	if strings.TrimSpace(text) == "" {
		return "[" + name + "]: #" + key
	}
	return strings.TrimRight(text, "\n") + "\n\n[" + name + "]: #" + key
}

func card(c tCard) tracker.Card {
	desc, key := split(taskMarker, c.Desc)
	out := tracker.Card{ID: c.ID, Key: key, Board: c.IDBoard, List: c.IDList, Title: c.Name, Description: desc,
		Assignees: nonNil(c.IDMembers), Labels: nonNil(c.IDLabels), Checklist: []tracker.CheckItem{}, Position: c.Pos, Archived: c.Closed,
		Version: c.DateLastActivity, UpdatedAt: c.DateLastActivity, URL: c.ShortURL}
	if c.Due != nil {
		out.Due = *c.Due
	}
	if c.Start != nil {
		out.Start = *c.Start
	}
	if cl := checklistOf(c); cl != nil {
		items := slices.Clone(cl.CheckItems)
		slices.SortStableFunc(items, func(a, b tCheckItem) int {
			switch {
			case a.Pos < b.Pos:
				return -1
			case a.Pos > b.Pos:
				return 1
			}
			return 0
		})
		for _, it := range items {
			out.Checklist = append(out.Checklist, tracker.CheckItem{ID: it.ID, Text: it.Name, Done: it.State == "complete"})
		}
	}
	return out
}

func checklistOf(c tCard) *tChecklist {
	for i := range c.Checklists {
		if c.Checklists[i].Name == ChecklistName {
			return &c.Checklists[i]
		}
	}
	if len(c.Checklists) > 0 {
		return &c.Checklists[0]
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func reply(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	return raw, err
}

func decode[T any](args json.RawMessage) (T, error) {
	var v T
	if len(args) > 0 {
		if err := json.Unmarshal(args, &v); err != nil {
			return v, connectors.Errorf(connectors.CodeInvalid, "the arguments are not valid: %v", err)
		}
	}
	return v, nil
}

// Call runs one tasks.tracker operation.
func (s *Session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	switch op {
	case "check":
		var me tMember
		if err := s.do(ctx, http.MethodGet, "/members/me", url.Values{"fields": {"username"}}, nil, &me); err != nil {
			return nil, err
		}
		return reply(map[string]any{"ok": true})
	case "board.list":
		in, err := decode[struct{ Archived bool }](args)
		if err != nil {
			return nil, err
		}
		filter := "open"
		if in.Archived {
			filter = "all"
		}
		var bs []tBoard
		if err := s.do(ctx, http.MethodGet, "/members/me/boards", url.Values{"fields": {"closed,name,url"}, "filter": {filter}}, nil, &bs); err != nil {
			return nil, err
		}
		out := []tracker.Board{}
		for _, b := range bs {
			out = append(out, tracker.Board{ID: b.ID, Name: b.Name, URL: b.URL, Archived: b.Closed})
		}
		return reply(map[string]any{"boards": out})
	case "board.get":
		in, err := decode[struct{ ID string }](args)
		if err != nil {
			return nil, err
		}
		var b tBoard
		q := url.Values{"fields": {"closed,name,url"}, "lists": {"open"}, "list_fields": {"closed,name,pos"}, "labels": {"all"}, "label_fields": {"color,name"},
			"members": {"all"}, "member_fields": {"fullName,username"}}
		if err := s.do(ctx, http.MethodGet, "/boards/"+url.PathEscape(in.ID), q, nil, &b); err != nil {
			return nil, err
		}
		lists := []tracker.List{}
		for _, l := range b.Lists {
			lists = append(lists, tracker.List{ID: l.ID, Name: l.Name, Position: l.Pos, Closed: l.Closed})
		}
		slices.SortStableFunc(lists, func(a, b tracker.List) int {
			switch {
			case a.Position < b.Position:
				return -1
			case a.Position > b.Position:
				return 1
			}
			return 0
		})
		labels := []tracker.Label{}
		for _, l := range b.Labels {
			labels = append(labels, tracker.Label{ID: l.ID, Name: l.Name, Color: l.Color})
		}
		return reply(map[string]any{"board": tracker.Board{ID: b.ID, Name: b.Name, URL: b.URL, Archived: b.Closed}, "lists": lists, "labels": labels, "members": members(b.Members)})
	case "member.list":
		in, err := decode[struct{ Board string }](args)
		if err != nil {
			return nil, err
		}
		var ms []tMember
		if err := s.do(ctx, http.MethodGet, "/boards/"+url.PathEscape(in.Board)+"/members", url.Values{"fields": {"fullName,username"}}, nil, &ms); err != nil {
			return nil, err
		}
		return reply(map[string]any{"members": members(ms)})
	case "card.list":
		return s.listCards(ctx, args)
	case "card.get":
		in, err := decode[struct{ ID string }](args)
		if err != nil {
			return nil, err
		}
		c, err := s.get(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		return reply(card(c))
	case "card.create":
		return s.create(ctx, args)
	case "card.update":
		return s.update(ctx, args)
	case "card.move":
		in, err := decode[struct {
			ID, List, Version string
			Position         *float64
		}](args)
		if err != nil {
			return nil, err
		}
		if in.Version != "" {
			if _, err := s.current(ctx, in.ID, in.Version); err != nil {
				return nil, err
			}
		}
		body := map[string]any{"idList": in.List, "pos": "bottom"}
		if in.Position != nil {
			body["pos"] = *in.Position
		}
		var c tCard
		if err := s.do(ctx, http.MethodPut, "/cards/"+url.PathEscape(in.ID), nil, body, &c); err != nil {
			return nil, err
		}
		return s.final(ctx, c, false)
	case "card.archive":
		in, err := decode[struct{ ID string }](args)
		if err != nil {
			return nil, err
		}
		c, err := s.get(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if !c.Closed {
			if err := s.do(ctx, http.MethodPut, "/cards/"+url.PathEscape(in.ID), nil, map[string]any{"closed": true}, &c); err != nil {
				return nil, err
			}
			return s.final(ctx, c, false)
		}
		return reply(card(c))
	case "comment.list":
		in, err := decode[struct{ Card string }](args)
		if err != nil {
			return nil, err
		}
		cs, err := s.comments(ctx, in.Card)
		if err != nil {
			return nil, err
		}
		return reply(map[string]any{"comments": cs})
	case "comment.create":
		return s.comment(ctx, args)
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "tasks.trello has no operation %s", op)
}

func members(ms []tMember) []tracker.Member {
	out := []tracker.Member{}
	for _, m := range ms {
		out = append(out, tracker.Member{ID: m.ID, Name: m.FullName, Username: m.Username})
	}
	return out
}

func (s *Session) get(ctx context.Context, id string) (tCard, error) {
	var c tCard
	err := s.do(ctx, http.MethodGet, "/cards/"+url.PathEscape(id), cardQuery(), nil, &c)
	return c, err
}

// current reads a card and refuses with conflict when it moved past
// version.
func (s *Session) current(ctx context.Context, id, version string) (tCard, error) {
	c, err := s.get(ctx, id)
	if err != nil {
		return c, err
	}
	if version != "" && c.DateLastActivity != version {
		return c, connectors.Errorf(connectors.CodeConflict, "the card changed in Trello since %s (now %s)", version, c.DateLastActivity)
	}
	return c, nil
}

// final gives a written card: a write's answer has no checklists, so the
// card is read again when its checklist matters.
func (s *Session) final(ctx context.Context, c tCard, reread bool) (json.RawMessage, error) {
	if reread {
		var err error
		if c, err = s.get(ctx, c.ID); err != nil {
			return nil, err
		}
	}
	return reply(card(c))
}

// listCards pages through a board's cards by Trello's own paging: limit,
// and before the last page's smallest id (ids sort by when they were
// made).
func (s *Session) listCards(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	in, err := decode[struct {
		Board, Cursor string
		Limit         int
		Archived      bool
	}](args)
	if err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	filter := "open"
	if in.Archived {
		filter = "all"
	}
	q := cardQuery()
	q.Set("limit", strconv.Itoa(limit))
	if in.Cursor != "" {
		q.Set("before", in.Cursor)
	}
	var cs []tCard
	if err := s.do(ctx, http.MethodGet, "/boards/"+url.PathEscape(in.Board)+"/cards/"+filter, q, nil, &cs); err != nil {
		return nil, err
	}
	out := []tracker.Card{}
	next := ""
	for _, c := range cs {
		out = append(out, card(c))
		if next == "" || c.ID < next {
			next = c.ID
		}
	}
	if len(cs) < limit {
		next = ""
	}
	return reply(map[string]any{"cards": out, "cursor": next})
}

type fields struct {
	Title       *string             `json:"title"`
	Description *string             `json:"description"`
	Assignees   *[]string           `json:"assignees"`
	Due         *string             `json:"due"`
	Start       *string             `json:"start"`
	Labels      *[]string           `json:"labels"`
	Checklist   *[]tracker.CheckItem `json:"checklist"`
}

// body gives Trello's PUT or POST fields for ours; desc keeps key's marker.
func (f fields) body(key string) map[string]any {
	b := map[string]any{}
	if f.Title != nil {
		b["name"] = *f.Title
	}
	if f.Description != nil {
		b["desc"] = withMarker(*f.Description, "axiom-task", key)
	}
	if f.Assignees != nil {
		b["idMembers"] = strings.Join(*f.Assignees, ",")
	}
	if f.Labels != nil {
		b["idLabels"] = strings.Join(*f.Labels, ",")
	}
	for k, v := range map[string]*string{"due": f.Due, "start": f.Start} {
		if v != nil {
			if *v == "" {
				b[k] = nil
			} else {
				b[k] = *v
			}
		}
	}
	return b
}

func (s *Session) create(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	in, err := decode[struct {
		Key, Board, List string
		Position         *float64
		fields
	}](args)
	if err != nil {
		return nil, err
	}
	if in.Key == "" || in.List == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "card.create needs a key and a list")
	}
	if id, err := s.findCard(ctx, in.Board, in.Key); err != nil {
		return nil, err
	} else if id != "" {
		c, err := s.get(ctx, id)
		if err != nil {
			return nil, err
		}
		return reply(card(c))
	}
	desc := ""
	if in.Description != nil {
		desc = *in.Description
	}
	in.Description = &desc
	body := in.fields.body(in.Key)
	body["idList"] = in.List
	body["pos"] = "bottom"
	if in.Position != nil {
		body["pos"] = *in.Position
	}
	for k, v := range body {
		if v == nil {
			delete(body, k)
		}
	}
	var c tCard
	if err := s.do(ctx, http.MethodPost, "/cards", nil, body, &c); err != nil {
		return nil, err
	}
	s.remember(in.Key, c.ID)
	if in.Checklist != nil && len(*in.Checklist) > 0 {
		if err := s.setChecklist(ctx, c, *in.Checklist); err != nil {
			return nil, err
		}
		return s.final(ctx, c, true)
	}
	return reply(card(c))
}

func (s *Session) remember(key, id string) {
	s.mu.Lock()
	s.made[key] = id
	s.mu.Unlock()
}

// findCard gives the card made with key: one this session made, else one
// whose description holds the key's marker (archived ones too).
func (s *Session) findCard(ctx context.Context, board, key string) (string, error) {
	s.mu.Lock()
	id := s.made[key]
	s.mu.Unlock()
	if id != "" {
		return id, nil
	}
	var cs []struct{ ID, Desc string }
	if err := s.do(ctx, http.MethodGet, "/boards/"+url.PathEscape(board)+"/cards/all", url.Values{"fields": {"desc"}}, nil, &cs); err != nil {
		return "", err
	}
	for _, c := range cs {
		if _, k := split(taskMarker, c.Desc); k == key {
			s.remember(key, c.ID)
			return c.ID, nil
		}
	}
	return "", nil
}

func (s *Session) update(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	in, err := decode[struct {
		ID, Version string
		Set         fields
	}](args)
	if err != nil {
		return nil, err
	}
	var cur tCard
	if in.Version != "" || in.Set.Description != nil || in.Set.Checklist != nil {
		if cur, err = s.current(ctx, in.ID, in.Version); err != nil {
			return nil, err
		}
	}
	_, key := split(taskMarker, cur.Desc)
	body := in.Set.body(key)
	c := cur
	if len(body) > 0 {
		if err := s.do(ctx, http.MethodPut, "/cards/"+url.PathEscape(in.ID), nil, body, &c); err != nil {
			return nil, err
		}
	}
	if in.Set.Checklist != nil {
		if err := s.setChecklist(ctx, cur, *in.Set.Checklist); err != nil {
			return nil, err
		}
		return s.final(ctx, c, true)
	}
	if len(body) == 0 {
		return s.final(ctx, tCard{ID: in.ID}, true)
	}
	return s.final(ctx, c, false)
}

// setChecklist makes the card's checklist hold items, in order: changed
// items are updated, missing ones deleted, new ones added at the end.
func (s *Session) setChecklist(ctx context.Context, c tCard, items []tracker.CheckItem) error {
	cl := checklistOf(c)
	if cl == nil {
		if len(items) == 0 {
			return nil
		}
		var made tChecklist
		if err := s.do(ctx, http.MethodPost, "/cards/"+url.PathEscape(c.ID)+"/checklists", nil, map[string]any{"name": ChecklistName}, &made); err != nil {
			return err
		}
		cl = &made
	}
	have := map[string]tCheckItem{}
	for _, it := range cl.CheckItems {
		have[it.ID] = it
	}
	keep := map[string]bool{}
	for i, it := range items {
		state := "incomplete"
		if it.Done {
			state = "complete"
		}
		pos := float64(16384 * (i + 1))
		if old, ok := have[it.ID]; ok && it.ID != "" {
			keep[it.ID] = true
			if old.Name != it.Text || old.State != state || old.Pos != pos {
				if err := s.do(ctx, http.MethodPut, "/cards/"+url.PathEscape(c.ID)+"/checkItem/"+url.PathEscape(it.ID), nil,
					map[string]any{"name": it.Text, "state": state, "pos": pos}, nil); err != nil {
					return err
				}
			}
			continue
		}
		if err := s.do(ctx, http.MethodPost, "/checklists/"+url.PathEscape(cl.ID)+"/checkItems", nil,
			map[string]any{"name": it.Text, "checked": it.Done, "pos": pos}, nil); err != nil {
			return err
		}
	}
	for _, it := range cl.CheckItems {
		if !keep[it.ID] {
			if err := s.do(ctx, http.MethodDelete, "/cards/"+url.PathEscape(c.ID)+"/checkItem/"+url.PathEscape(it.ID), nil, nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func comment(a tAction) tracker.Comment {
	text, key := split(commentMarker, a.Data.Text)
	return tracker.Comment{ID: a.ID, Key: key, Card: a.Data.Card.ID, Author: a.IDMemberCreator, Text: text, CreatedAt: a.Date}
}

// comments gives a card's comments, oldest first (Trello gives its
// actions newest first).
func (s *Session) comments(ctx context.Context, cardID string) ([]tracker.Comment, error) {
	var as []tAction
	if err := s.do(ctx, http.MethodGet, "/cards/"+url.PathEscape(cardID)+"/actions", url.Values{"filter": {"commentCard"}, "limit": {"1000"}}, nil, &as); err != nil {
		return nil, err
	}
	out := []tracker.Comment{}
	for i := len(as) - 1; i >= 0; i-- {
		out = append(out, comment(as[i]))
	}
	return out, nil
}

func (s *Session) comment(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	in, err := decode[struct{ Key, Card, Text string }](args)
	if err != nil {
		return nil, err
	}
	if in.Key == "" || in.Text == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "comment.create needs a key and a text")
	}
	cs, err := s.comments(ctx, in.Card)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if c.Key == in.Key {
			return reply(c)
		}
	}
	var a tAction
	if err := s.do(ctx, http.MethodPost, "/cards/"+url.PathEscape(in.Card)+"/actions/comments", nil, map[string]any{"text": withMarker(in.Text, "axiom-comment", in.Key)}, &a); err != nil {
		return nil, err
	}
	return reply(comment(a))
}

// The action types Watch reads.
var watched = []string{"createCard", "updateCard", "commentCard", "copyCard", "moveCardToBoard", "addMemberToCard", "removeMemberFromCard",
	"addLabelToCard", "removeLabelFromCard", "addChecklistToCard", "removeChecklistFromCard", "createCheckItem", "updateCheckItem",
	"updateCheckItemStateOnCard", "deleteCheckItem"}

// The fields of ours an updateCard's old values name.
var trelloField = map[string]string{"name": "title", "desc": "description", "idList": "list", "pos": "position", "idMembers": "assignees",
	"idLabels": "labels", "due": "due", "start": "start", "closed": "archived"}

// Watch polls every open board's actions since the cursor (each board's
// last action id), reporting card.changed and comment.created. A board
// seen first starts at its newest action, so a new connection reports
// what happens from now; the import reads the rest.
func (s *Session) Watch(ctx adapter.Context, cursor json.RawMessage, emit func(adapter.Event) error) error {
	last := map[string]string{}
	if len(cursor) > 0 {
		_ = json.Unmarshal(cursor, &last)
	}
	for {
		if err := s.watchOnce(ctx, last, emit); err != nil {
			if ce := connectors.AsError(err); ce == nil || !ce.Retry {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.poll):
		}
	}
}

func (s *Session) watchOnce(ctx adapter.Context, last map[string]string, emit func(adapter.Event) error) error {
	var bs []tBoard
	if err := s.do(ctx, http.MethodGet, "/members/me/boards", url.Values{"fields": {"id"}, "filter": {"open"}}, nil, &bs); err != nil {
		return err
	}
	for _, b := range bs {
		q := url.Values{"filter": {strings.Join(watched, ",")}, "limit": {"1000"}}
		since, seen := last[b.ID]
		if !seen {
			q.Set("limit", "1")
		} else if since != "" {
			q.Set("since", since)
		}
		var as []tAction
		if err := s.do(ctx, http.MethodGet, "/boards/"+url.PathEscape(b.ID)+"/actions", q, nil, &as); err != nil {
			return err
		}
		if !seen {
			last[b.ID] = ""
			if len(as) > 0 {
				last[b.ID] = as[0].ID
			}
			continue
		}
		for i := len(as) - 1; i >= 0; i-- {
			a := as[i]
			last[b.ID] = a.ID
			cur, _ := json.Marshal(last)
			name, payload := event(b.ID, a)
			raw, _ := json.Marshal(payload)
			at, _ := time.Parse(time.RFC3339Nano, a.Date)
			if err := emit(adapter.Event{Contract: tracker.Ref, Name: name, ID: a.ID, Payload: raw, At: at, Cursor: cur}); err != nil {
				return err
			}
		}
	}
	return nil
}

// event turns an action into card.changed or comment.created; the card's
// version after it is the action's date (its dateLastActivity).
func event(board string, a tAction) (string, map[string]any) {
	if a.Type == "commentCard" {
		return "comment.created", map[string]any{"id": a.ID, "card": a.Data.Card.ID, "board": board, "comment": a.ID, "version": a.Date, "createdAt": a.Date}
	}
	change := tracker.ChangeUpdated
	fs := []string{}
	switch a.Type {
	case "createCard", "copyCard", "moveCardToBoard":
		change = tracker.ChangeCreated
	case "updateCard":
		for k := range a.Data.Old {
			if f := trelloField[k]; f != "" {
				fs = append(fs, f)
			}
		}
		slices.Sort(fs)
		if _, ok := a.Data.Old["idList"]; ok {
			change = tracker.ChangeMoved
		}
		if raw, ok := a.Data.Old["closed"]; ok && string(raw) == "false" {
			change = tracker.ChangeArchived
		}
	case "addMemberToCard", "removeMemberFromCard":
		fs = []string{"assignees"}
	case "addLabelToCard", "removeLabelFromCard":
		fs = []string{"labels"}
	default:
		fs = []string{"checklist"}
	}
	return "card.changed", map[string]any{"id": a.ID, "card": a.Data.Card.ID, "board": board, "change": change, "version": a.Date, "updatedAt": a.Date, "fields": fs}
}
