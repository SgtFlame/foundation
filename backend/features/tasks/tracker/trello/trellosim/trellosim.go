// Package trellosim is a small Trello REST API (v1) in memory, written
// from Trello's API reference (forge 30i): the endpoints the tasks.trello
// type calls, with Trello's shapes, its plain-text errors ("invalid id"
// for a malformed id, a 404 for an unknown one), comments as commentCard
// actions and card changes as the board's actions. It is what the
// cassettes in ../testdata are written from (until a real board records
// them, deferred to Dec 15), and what end-to-end tests run against.
//
// Ids are 24 hex digits that sort in the order they were made, and the
// clock starts at 2026-11-27T10:00:00Z and moves one second per change,
// so a run against a fresh server writes the same cassette every time.
package trellosim

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Start is the simulated clock's first time.
var Start = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

// Me is the token's member.
const (
	MeName     = "Ada Lovelace"
	MeUsername = "ada"
)

// Board is a seeded board.
type Board struct {
	ID      string
	Name    string
	Lists   []string // list names, in order
	Labels  []string
	Members []string // full names beside the token's member
	Cards   int      // cards spread over the lists, titled "Card <n>"
}

type checkItem struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	State string  `json:"state"` // complete | incomplete
	Pos   float64 `json:"pos"`
}

type checklist struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	IDCard     string      `json:"idCard"`
	CheckItems []checkItem `json:"checkItems"`
}

// Card is Trello's card.
type Card struct {
	ID               string       `json:"id"`
	IDShort          int          `json:"idShort"`
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
	Checklists       []*checklist `json:"checklists,omitempty"`
}

type list struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Pos     float64 `json:"pos"`
	Closed  bool    `json:"closed"`
	IDBoard string  `json:"idBoard"`
}

type label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type member struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Username string `json:"username"`
}

type board struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Closed  bool     `json:"closed"`
	lists   []*list  // in order
	labels  []label  //
	members []member //
	actions []action // oldest first
}

type action struct {
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Date            string         `json:"date"`
	IDMemberCreator string         `json:"idMemberCreator"`
	Data            map[string]any `json:"data"`
}

// Server is the simulated API, an http.Handler serving /1/...
type Server struct {
	mu         sync.Mutex
	n          int
	clock      time.Time
	me         member
	boards     map[string]*board
	order      []string // board ids in the order made
	cards      map[string]*Card
	checklists map[string]*checklist
	throttle   int // the next requests answered 429
	// Requests counts the requests served, for tests.
	Requests int
}

// New makes an empty server whose token belongs to Ada Lovelace.
func New() *Server {
	s := &Server{clock: Start, boards: map[string]*board{}, cards: map[string]*Card{}, checklists: map[string]*checklist{}}
	s.me = member{ID: s.id(), FullName: MeName, Username: MeUsername}
	return s
}

func (s *Server) id() string {
	s.n++
	return fmt.Sprintf("6564%020x", s.n)
}

func (s *Server) tick() string {
	s.clock = s.clock.Add(time.Second)
	return s.clock.Format("2006-01-02T15:04:05.000Z")
}

// Seed makes a board with its lists, labels, members and cards, and
// gives the ids made: the board's, then each list's.
func (s *Server) Seed(b Board) (string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := b.ID
	if id == "" {
		id = s.id()
	}
	bd := &board{ID: id, Name: b.Name, URL: "https://trello.com/b/" + id[len(id)-8:], members: []member{s.me}}
	s.boards[id] = bd
	s.order = append(s.order, id)
	var lists []string
	for i, name := range b.Lists {
		l := &list{ID: s.id(), Name: name, Pos: float64(16384 * (i + 1)), IDBoard: id}
		bd.lists = append(bd.lists, l)
		lists = append(lists, l.ID)
	}
	colors := []string{"green", "yellow", "orange", "red", "purple", "blue"}
	for i, name := range b.Labels {
		bd.labels = append(bd.labels, label{ID: s.id(), Name: name, Color: colors[i%len(colors)]})
	}
	for _, name := range b.Members {
		bd.members = append(bd.members, member{ID: s.id(), FullName: name, Username: strings.ToLower(strings.Fields(name)[0])})
	}
	for i := 1; i <= b.Cards && len(bd.lists) > 0; i++ {
		l := bd.lists[(i-1)%len(bd.lists)]
		s.newCard(bd, l, fmt.Sprintf("Card %d", i), "", s.bottom(l.ID))
	}
	return id, lists
}

// Change changes a card as someone in Trello would (its name, desc or
// idList), recording the action; it gives the card's new dateLastActivity.
func (s *Server) Change(card string, set map[string]any) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cards[card]
	if c == nil {
		return ""
	}
	s.update(c, set)
	return c.DateLastActivity
}

// Throttle answers the next n requests with 429.
func (s *Server) Throttle(n int) {
	s.mu.Lock()
	s.throttle = n
	s.mu.Unlock()
}

// Cards gives a board's open cards, for tests.
func (s *Server) Cards(boardID string) []Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Card
	for _, c := range s.cards {
		if c.IDBoard == boardID && !c.Closed {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Server) bottom(listID string) float64 {
	pos := 0.0
	for _, c := range s.cards {
		if c.IDList == listID && c.Pos > pos {
			pos = c.Pos
		}
	}
	return pos + 16384
}

func (s *Server) newCard(bd *board, l *list, name, desc string, pos float64) *Card {
	n := 0
	for _, c := range s.cards {
		if c.IDBoard == bd.ID && c.IDShort > n {
			n = c.IDShort
		}
	}
	id := s.id()
	c := &Card{ID: id, IDShort: n + 1, Name: name, Desc: desc, IDBoard: bd.ID, IDList: l.ID, IDMembers: []string{}, IDLabels: []string{}, Pos: pos,
		ShortURL: "https://trello.com/c/" + id[len(id)-8:]}
	s.cards[id] = c
	c.DateLastActivity = s.record(bd, "createCard", map[string]any{"card": cardRef(c), "list": map[string]any{"id": l.ID, "name": l.Name}})
	return c
}

func cardRef(c *Card) map[string]any {
	return map[string]any{"id": c.ID, "name": c.Name, "idShort": c.IDShort}
}

// record adds an action to the board and gives its date; under s.mu.
func (s *Server) record(bd *board, typ string, data map[string]any) string {
	date := s.tick()
	data["board"] = map[string]any{"id": bd.ID, "name": bd.Name}
	bd.actions = append(bd.actions, action{ID: s.id(), Type: typ, Date: date, IDMemberCreator: s.me.ID, Data: data})
	return date
}

// update applies Trello's PUT /cards fields, recording one updateCard
// action with the old values; under s.mu.
func (s *Server) update(c *Card, set map[string]any) {
	old := map[string]any{}
	for k, v := range set {
		switch k {
		case "name":
			if str(v) != c.Name {
				old[k] = c.Name
				c.Name = str(v)
			}
		case "desc":
			if str(v) != c.Desc {
				old[k] = c.Desc
				c.Desc = str(v)
			}
		case "idList":
			if str(v) != c.IDList {
				old[k] = c.IDList
				c.IDList = str(v)
			}
		case "pos":
			p := s.position(c.IDList, v)
			if p != c.Pos {
				old[k] = c.Pos
				c.Pos = p
			}
		case "closed":
			b, _ := v.(bool)
			if b != c.Closed {
				old[k] = c.Closed
				c.Closed = b
			}
		case "idMembers":
			ids := splitIDs(v)
			if !slices.Equal(ids, c.IDMembers) {
				old[k] = c.IDMembers
				c.IDMembers = ids
			}
		case "idLabels":
			ids := splitIDs(v)
			if !slices.Equal(ids, c.IDLabels) {
				old[k] = c.IDLabels
				c.IDLabels = ids
			}
		case "due", "start":
			var p *string
			if v != nil && str(v) != "" {
				x := str(v)
				p = &x
			}
			cur := &c.Due
			if k == "start" {
				cur = &c.Start
			}
			if deref(*cur) != deref(p) {
				old[k] = *cur
				*cur = p
			}
		}
	}
	if len(old) == 0 {
		return
	}
	data := map[string]any{"card": cardRef(c), "old": old}
	if _, ok := old["idList"]; ok {
		data["listBefore"] = map[string]any{"id": old["idList"]}
		data["listAfter"] = map[string]any{"id": c.IDList}
	}
	c.DateLastActivity = s.record(s.boards[c.IDBoard], "updateCard", data)
}

func (s *Server) position(listID string, v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		if x == "top" {
			top := 0.0
			for _, c := range s.cards {
				if c.IDList == listID && (top == 0 || c.Pos < top) {
					top = c.Pos
				}
			}
			return top / 2
		}
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f
		}
	}
	return s.bottom(listID)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// splitIDs reads Trello's comma-separated id lists (or a JSON array).
func splitIDs(v any) []string {
	out := []string{}
	switch x := v.(type) {
	case string:
		for _, p := range strings.Split(x, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	case []any:
		for _, p := range x {
			out = append(out, str(p))
		}
	}
	return out
}

func validID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func text(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg)
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// ServeHTTP answers /1/... as Trello does.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests++
	if s.throttle > 0 {
		s.throttle--
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"API_TOKEN_LIMIT_EXCEEDED","message":"Rate limit exceeded"}`)
		return
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "OAuth ") || !strings.Contains(auth, "oauth_consumer_key=") || !strings.Contains(auth, "oauth_token=") ||
		strings.Contains(auth, `oauth_token="bad"`) {
		text(w, http.StatusUnauthorized, "invalid token")
		return
	}
	in := map[string]any{}
	for k, v := range r.URL.Query() {
		in[k] = v[0]
	}
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				text(w, http.StatusBadRequest, "invalid value for body")
				return
			}
			for k, v := range body {
				in[k] = v
			}
		}
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/1"), "/"), "/")
	s.route(w, r.Method, parts, in, r.URL.Query())
}

func (s *Server) route(w http.ResponseWriter, method string, p []string, in map[string]any, q url.Values) {
	get := method == http.MethodGet
	switch {
	case get && len(p) == 2 && p[0] == "members" && p[1] == "me":
		reply(w, s.me)
	case get && len(p) == 3 && p[0] == "members" && p[1] == "me" && p[2] == "boards":
		out := []*board{}
		for _, id := range s.order {
			if b := s.boards[id]; q.Get("filter") != "open" || !b.Closed {
				out = append(out, b)
			}
		}
		reply(w, out)
	case len(p) >= 2 && p[0] == "boards":
		if !validID(p[1]) {
			text(w, http.StatusBadRequest, "invalid id")
			return
		}
		b := s.boards[p[1]]
		if b == nil {
			text(w, http.StatusNotFound, "The requested resource was not found.")
			return
		}
		s.boardRoute(w, get, b, p[2:], q)
	case method == http.MethodPost && len(p) == 1 && p[0] == "cards":
		s.createCard(w, in)
	case len(p) >= 2 && p[0] == "cards":
		if !validID(p[1]) {
			text(w, http.StatusBadRequest, "invalid id")
			return
		}
		c := s.cards[p[1]]
		if c == nil {
			text(w, http.StatusNotFound, "The requested resource was not found.")
			return
		}
		s.cardRoute(w, method, c, p[2:], in, q)
	case len(p) >= 2 && p[0] == "checklists":
		cl := s.checklists[p[1]]
		if cl == nil {
			text(w, http.StatusNotFound, "The requested resource was not found.")
			return
		}
		s.checklistRoute(w, method, cl, p[2:], in)
	default:
		text(w, http.StatusNotFound, "Cannot "+method+" /1/"+strings.Join(p, "/"))
	}
}

func (s *Server) boardRoute(w http.ResponseWriter, get bool, b *board, p []string, q url.Values) {
	if !get {
		text(w, http.StatusNotFound, "not served by the simulator")
		return
	}
	switch {
	case len(p) == 0:
		out := map[string]any{"id": b.ID, "name": b.Name, "url": b.URL, "closed": b.Closed}
		if q.Get("lists") != "" {
			ls := []*list{}
			for _, l := range b.lists {
				if q.Get("lists") != "open" || !l.Closed {
					ls = append(ls, l)
				}
			}
			out["lists"] = ls
		}
		if q.Get("labels") != "" {
			out["labels"] = b.labels
		}
		if q.Get("members") != "" {
			out["members"] = b.members
		}
		reply(w, out)
	case len(p) == 1 && p[0] == "members":
		reply(w, b.members)
	case len(p) == 2 && p[0] == "cards":
		// Trello's paging: limit, and before (an id) for the cards made
		// before it, newest first.
		var all []*Card
		for _, c := range s.cards {
			if c.IDBoard == b.ID && (p[1] == "all" || !c.Closed) {
				all = append(all, c)
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
		if before := q.Get("before"); before != "" {
			all = slices.DeleteFunc(all, func(c *Card) bool { return c.ID >= before })
		}
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n < len(all) {
			all = all[:n]
		}
		out := make([]Card, 0, len(all))
		for _, c := range all {
			out = append(out, s.view(c, q))
		}
		reply(w, out)
	case len(p) == 1 && p[0] == "actions":
		filter := strings.Split(q.Get("filter"), ",")
		out := []action{}
		for i := len(b.actions) - 1; i >= 0; i-- { // newest first
			a := b.actions[i]
			if since := q.Get("since"); since != "" && a.ID <= since {
				break
			}
			if q.Get("filter") == "" || slices.Contains(filter, a.Type) {
				out = append(out, a)
			}
		}
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n < len(out) {
			out = out[:n]
		}
		reply(w, out)
	default:
		text(w, http.StatusNotFound, "not served by the simulator")
	}
}

// view is a card as a read gives it, with its checklists when asked.
func (s *Server) view(c *Card, q url.Values) Card {
	v := *c
	v.IDMembers = append([]string{}, c.IDMembers...)
	v.IDLabels = append([]string{}, c.IDLabels...)
	v.Checklists = nil
	if q.Get("checklists") == "all" {
		v.Checklists = []*checklist{}
		for _, cl := range c.Checklists {
			cp := *cl
			cp.CheckItems = append([]checkItem{}, cl.CheckItems...)
			v.Checklists = append(v.Checklists, &cp)
		}
	}
	return v
}

func (s *Server) createCard(w http.ResponseWriter, in map[string]any) {
	lid := str(in["idList"])
	if !validID(lid) {
		text(w, http.StatusBadRequest, "invalid value for idList")
		return
	}
	var l *list
	var bd *board
	for _, b := range s.boards {
		for _, x := range b.lists {
			if x.ID == lid {
				l, bd = x, b
			}
		}
	}
	if l == nil {
		text(w, http.StatusNotFound, "The requested resource was not found.")
		return
	}
	c := s.newCard(bd, l, str(in["name"]), str(in["desc"]), s.position(l.ID, in["pos"]))
	// the other fields set at creation belong to the create
	for _, k := range []string{"idMembers", "idLabels"} {
		if v, ok := in[k]; ok {
			ids := splitIDs(v)
			if k == "idMembers" {
				c.IDMembers = ids
			} else {
				c.IDLabels = ids
			}
		}
	}
	for _, k := range []string{"due", "start"} {
		if x := str(in[k]); x != "" {
			if k == "due" {
				c.Due = &x
			} else {
				c.Start = &x
			}
		}
	}
	reply(w, s.view(c, url.Values{}))
}

func (s *Server) cardRoute(w http.ResponseWriter, method string, c *Card, p []string, in map[string]any, q url.Values) {
	switch {
	case method == http.MethodGet && len(p) == 0:
		reply(w, s.view(c, q))
	case method == http.MethodPut && len(p) == 0:
		set := map[string]any{}
		for _, k := range []string{"name", "desc", "idList", "pos", "closed", "idMembers", "idLabels", "due", "start"} {
			if v, ok := in[k]; ok {
				if k == "closed" {
					if s, ok := v.(string); ok {
						v = s == "true"
					}
				}
				set[k] = v
			}
		}
		s.update(c, set)
		reply(w, s.view(c, url.Values{}))
	case method == http.MethodGet && len(p) == 1 && p[0] == "actions":
		filter := strings.Split(q.Get("filter"), ",")
		out := []action{}
		bd := s.boards[c.IDBoard]
		for i := len(bd.actions) - 1; i >= 0; i-- {
			a := bd.actions[i]
			ref, _ := a.Data["card"].(map[string]any)
			if ref == nil || ref["id"] != c.ID || (q.Get("filter") != "" && !slices.Contains(filter, a.Type)) {
				continue
			}
			out = append(out, a)
		}
		reply(w, out)
	case method == http.MethodPost && len(p) == 2 && p[0] == "actions" && p[1] == "comments":
		t := str(in["text"])
		if t == "" {
			text(w, http.StatusBadRequest, "invalid value for text")
			return
		}
		date := s.record(s.boards[c.IDBoard], "commentCard", map[string]any{"card": cardRef(c), "text": t})
		c.DateLastActivity = date
		bd := s.boards[c.IDBoard]
		reply(w, bd.actions[len(bd.actions)-1])
	case method == http.MethodGet && len(p) == 1 && p[0] == "checklists":
		reply(w, s.view(c, url.Values{"checklists": {"all"}}).Checklists)
	case method == http.MethodPost && len(p) == 1 && p[0] == "checklists":
		cl := &checklist{ID: s.id(), Name: str(in["name"]), IDCard: c.ID, CheckItems: []checkItem{}}
		c.Checklists = append(c.Checklists, cl)
		s.checklists[cl.ID] = cl
		c.DateLastActivity = s.record(s.boards[c.IDBoard], "addChecklistToCard", map[string]any{"card": cardRef(c), "checklist": map[string]any{"id": cl.ID, "name": cl.Name}})
		reply(w, cl)
	case method == http.MethodPut && len(p) == 2 && p[0] == "checkItem":
		cl, i := s.item(c, p[1])
		if cl == nil {
			text(w, http.StatusNotFound, "The requested resource was not found.")
			return
		}
		it := &cl.CheckItems[i]
		if v, ok := in["name"]; ok {
			it.Name = str(v)
		}
		if v, ok := in["state"]; ok {
			it.State = str(v)
		}
		if v, ok := in["pos"]; ok {
			if f, ok := v.(float64); ok {
				it.Pos = f
			}
		}
		c.DateLastActivity = s.record(s.boards[c.IDBoard], "updateCheckItemStateOnCard", map[string]any{"card": cardRef(c), "checkItem": map[string]any{"id": it.ID, "state": it.State}})
		reply(w, it)
	case method == http.MethodDelete && len(p) == 2 && p[0] == "checkItem":
		cl, i := s.item(c, p[1])
		if cl == nil {
			text(w, http.StatusNotFound, "The requested resource was not found.")
			return
		}
		cl.CheckItems = slices.Delete(cl.CheckItems, i, i+1)
		c.DateLastActivity = s.record(s.boards[c.IDBoard], "deleteCheckItem", map[string]any{"card": cardRef(c)})
		reply(w, map[string]any{"limits": map[string]any{}})
	default:
		text(w, http.StatusNotFound, "not served by the simulator")
	}
}

func (s *Server) item(c *Card, id string) (*checklist, int) {
	for _, cl := range c.Checklists {
		for i, it := range cl.CheckItems {
			if it.ID == id {
				return cl, i
			}
		}
	}
	return nil, 0
}

func (s *Server) checklistRoute(w http.ResponseWriter, method string, cl *checklist, p []string, in map[string]any) {
	if method != http.MethodPost || len(p) != 1 || p[0] != "checkItems" {
		text(w, http.StatusNotFound, "not served by the simulator")
		return
	}
	c := s.cards[cl.IDCard]
	state := "incomplete"
	if b, _ := in["checked"].(bool); b || in["checked"] == "true" {
		state = "complete"
	}
	pos := float64(16384 * (len(cl.CheckItems) + 1))
	it := checkItem{ID: s.id(), Name: str(in["name"]), State: state, Pos: pos}
	cl.CheckItems = append(cl.CheckItems, it)
	c.DateLastActivity = s.record(s.boards[c.IDBoard], "createCheckItem", map[string]any{"card": cardRef(c), "checkItem": map[string]any{"id": it.ID}})
	reply(w, it)
}
