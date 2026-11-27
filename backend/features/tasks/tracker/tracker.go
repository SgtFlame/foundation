// Package tracker is the tasks.tracker@1 connector contract (forge 30g,
// documentation/Tasks.md › Built in forge 30g): one contract for outside
// task trackers (Trello, Asana, Linear, Jira), so the two-way sync of 30h
// is the same for every one. Its suite is trackertest and its in-memory
// test server trackerfake.
//
// A card carries the outside version and updatedAt the tracker reports,
// so the sync tells its own echo from a real change, and card.update with
// a version is refused with conflict once the card moved on. card.create
// and comment.create are keyed by our id, so a retry makes one.
package tracker

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// The contract's reference.
const (
	ID  = "tasks.tracker"
	Ref = "tasks.tracker@1"
)

// Card change kinds a card.changed event reports.
const (
	ChangeCreated  = "created"
	ChangeUpdated  = "updated"
	ChangeMoved    = "moved"
	ChangeArchived = "archived"
)

// Board is an outside board (a Trello board, an Asana project).
type Board struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url,omitempty"`
	Archived bool   `json:"archived,omitempty"`
}

// List is a board's list or status column.
type List struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Position float64 `json:"position"`
	Closed   bool    `json:"closed,omitempty"`
}

// Label is a board's label.
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Member is someone on the board.
type Member struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
}

// CheckItem is one checklist item of a card.
type CheckItem struct {
	ID   string `json:"id,omitempty"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// Card is an outside card, issue or task. Version changes on every change
// the tracker records; UpdatedAt is by its clock.
type Card struct {
	ID          string      `json:"id"`
	Key         string      `json:"key,omitempty"` // ours, when we made it
	Board       string      `json:"board"`
	List        string      `json:"list"`
	Title       string      `json:"title"`
	Description string      `json:"description"` // Markdown
	Assignees   []string    `json:"assignees"`   // member ids
	Due         string      `json:"due,omitempty"`
	Start       string      `json:"start,omitempty"`
	Labels      []string    `json:"labels"` // label ids
	Checklist   []CheckItem `json:"checklist"`
	Position    float64     `json:"position"` // sorts the list's cards
	Archived    bool        `json:"archived"`
	Version     string      `json:"version"`
	UpdatedAt   string      `json:"updatedAt"`
	URL         string      `json:"url,omitempty"`
}

// Comment is a comment on a card.
type Comment struct {
	ID        string `json:"id"`
	Key       string `json:"key,omitempty"`
	Card      string `json:"card"`
	Author    string `json:"author"` // a member id
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

// CardFields are the fields card.update may set.
var CardFields = []string{"title", "description", "assignees", "due", "start", "labels", "checklist"}

const boardSchema = `{"type":"object","description":"A board (a Trello board, an Asana project, a Linear team)","properties":{
  "id":{"type":"string","description":"The board's outside id"},
  "name":{"type":"string","description":"Its name"},
  "url":{"type":"string","description":"Where it is opened in the tracker"},
  "archived":{"type":"boolean","description":"Whether it is archived"}},"required":["id","name"]}`

const listSchema = `{"type":"object","description":"A list or status column of a board","properties":{
  "id":{"type":"string","description":"The list's outside id"},
  "name":{"type":"string","description":"Its name, e.g. Doing"},
  "position":{"type":"number","description":"Sorts the board's lists, smallest first"},
  "closed":{"type":"boolean","description":"Whether it is closed (archived)"}},"required":["id","name","position"]}`

const labelSchema = `{"type":"object","description":"A label of a board","properties":{
  "id":{"type":"string","description":"The label's outside id"},
  "name":{"type":"string","description":"Its name"},
  "color":{"type":"string","description":"Its colour as the tracker names it"}},"required":["id","name"]}`

const memberSchema = `{"type":"object","description":"Someone on the board","properties":{
  "id":{"type":"string","description":"The member's outside id"},
  "name":{"type":"string","description":"Their full name"},
  "username":{"type":"string","description":"Their user name in the tracker"},
  "email":{"type":"string","description":"Their email address, when the tracker shows it"}},"required":["id","name"]}`

const checkItemSchema = `{"type":"object","description":"A checklist item","properties":{
  "id":{"type":"string","description":"The item's outside id; left out for a new one"},
  "text":{"type":"string","description":"What is to be done"},
  "done":{"type":"boolean","description":"Whether it is done"}},"required":["text","done"]}`

const cardSchema = `{"type":"object","description":"A card, issue or task as the tracker reports it","properties":{
  "id":{"type":"string","description":"The card's outside id"},
  "key":{"type":"string","description":"Our task id it was created with (card.create's key), when we made it"},
  "board":{"type":"string","description":"Its board's id"},
  "list":{"type":"string","description":"Its list or status's id"},
  "title":{"type":"string","description":"Its title"},
  "description":{"type":"string","description":"Its description, in Markdown"},
  "assignees":{"type":"array","items":{"type":"string"},"description":"The ids of the members it is assigned to"},
  "due":{"type":"string","description":"When it is due, RFC 3339 or YYYY-MM-DD; empty when none"},
  "start":{"type":"string","description":"When it starts, RFC 3339 or YYYY-MM-DD; empty when none"},
  "labels":{"type":"array","items":{"type":"string"},"description":"The ids of its labels"},
  "checklist":{"type":"array","items":` + checkItemSchema + `,"description":"Its checklist items, in order"},
  "position":{"type":"number","description":"Sorts its list's cards, smallest first"},
  "archived":{"type":"boolean","description":"Whether it is archived (closed)"},
  "version":{"type":"string","description":"The tracker's version of the card, changed by every change; card.update's version"},
  "updatedAt":{"type":"string","description":"When it last changed, RFC 3339, by the tracker's clock"},
  "url":{"type":"string","description":"Where it is opened in the tracker"}},"required":["id","board","list","title","version","updatedAt"]}`

const commentSchema = `{"type":"object","description":"A comment on a card","properties":{
  "id":{"type":"string","description":"The comment's outside id"},
  "key":{"type":"string","description":"Our id it was created with (comment.create's key), when we made it"},
  "card":{"type":"string","description":"The card's id"},
  "author":{"type":"string","description":"The member id of who wrote it"},
  "text":{"type":"string","description":"Its text, in Markdown"},
  "createdAt":{"type":"string","description":"When it was written, RFC 3339"}},"required":["id","card","text","createdAt"]}`

const (
	boardExample   = `{"id":"b1","name":"Website relaunch","url":"https://tracker.example/b/b1"}`
	cardExample    = `{"id":"c1","key":"7340032001","board":"b1","list":"l2","title":"Write the launch post","description":"Draft and review","assignees":["m1"],"due":"2026-12-01","labels":["lb1"],"checklist":[{"id":"ci1","text":"Outline","done":true}],"position":1024,"archived":false,"version":"3","updatedAt":"2026-11-27T10:00:00Z"}`
	commentExample = `{"id":"cm1","key":"7340032101","card":"c1","author":"m1","text":"Looks good","createdAt":"2026-11-27T10:05:00Z"}`
)

func object(props string, required ...string) json.RawMessage {
	req := ""
	if len(required) > 0 {
		b, _ := json.Marshal(required)
		req = `,"required":` + string(b)
	}
	return json.RawMessage(`{"type":"object","properties":{` + props + `}` + req + `}`)
}

// Contract is tasks.tracker@1.
func Contract() connectors.Contract {
	retryable := []string{connectors.CodeRateLimited, connectors.CodeUnavailable}
	reads := append([]string{connectors.CodeAuth, connectors.CodeNotFound}, retryable...)
	writes := append([]string{connectors.CodeAuth, connectors.CodeNotFound, connectors.CodeInvalid}, retryable...)
	idArg := func(what string) string { return `"id":{"type":"string","description":"The ` + what + `'s outside id"}` }
	fields := `"title":{"type":"string","description":"Its title"},
  "description":{"type":"string","description":"Its description, in Markdown"},
  "assignees":{"type":"array","items":{"type":"string"},"description":"Member ids to assign (member.list)"},
  "due":{"type":"string","description":"When it is due, RFC 3339 or YYYY-MM-DD; empty for none"},
  "start":{"type":"string","description":"When it starts, RFC 3339 or YYYY-MM-DD; empty for none"},
  "labels":{"type":"array","items":{"type":"string"},"description":"Label ids (board.get)"},
  "checklist":{"type":"array","items":` + checkItemSchema + `,"description":"The whole checklist, in order"}`
	return connectors.Contract{
		ID: ID, Version: 1,
		Meta: connectors.Meta{Title: "Task tracker", Description: "Reads and changes boards, lists, cards and comments in an outside task tracker (Trello, Asana, Linear, Jira), for two-way sync with Axiom's tasks. Cards carry the tracker's version and updatedAt, so a sync never echoes its own change back; creates are keyed by our id, so a retry makes one.", Tags: []string{"tasks", "tracker", "kanban", "sync", "project management"}},
		Operations: []connectors.Operation{
			{
				ID: "board.list", Title: "List boards", Description: "Lists the boards (projects) the connection can see.",
				Effect: connectors.EffectRead, Args: object(`"archived":{"type":"boolean","description":"Include archived boards too"}`),
				Result:   object(`"boards":{"type":"array","items":`+boardSchema+`,"description":"The boards"}`, "boards"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"boards":[` + boardExample + `]}`)}},
				Errors:   append([]string{connectors.CodeAuth}, retryable...),
			},
			{
				ID: "board.get", Title: "Get a board", Description: "Reads one board with its lists (or statuses) in order, its labels and its members, to map them to a project's statuses, labels and people.",
				Effect: connectors.EffectRead, Args: object(idArg("board"), "id"),
				Result: object(`"board":`+boardSchema+`,
  "lists":{"type":"array","items":`+listSchema+`,"description":"Its lists or statuses, in order"},
  "labels":{"type":"array","items":`+labelSchema+`,"description":"Its labels"},
  "members":{"type":"array","items":`+memberSchema+`,"description":"Its members"}`, "board", "lists", "labels", "members"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"b1"}`), Result: json.RawMessage(`{"board":` + boardExample + `,"lists":[{"id":"l1","name":"To do","position":1},{"id":"l2","name":"Doing","position":2}],"labels":[{"id":"lb1","name":"Launch","color":"green"}],"members":[{"id":"m1","name":"Ada Lovelace","username":"ada"}]}`)}},
				Errors:   reads,
			},
			{
				ID: "card.list", Title: "List cards", Description: "Lists a board's cards a page at a time, each with its version and updatedAt; pass the cursor it gives to get the next page, until it gives none. The pages together hold every card once.",
				Effect: connectors.EffectRead,
				Args: object(`"board":{"type":"string","description":"The board's id"},
  "cursor":{"type":"string","description":"The cursor of the page before; left out for the first"},
  "limit":{"type":"integer","minimum":1,"maximum":500,"description":"Cards per page; 100 when left out"},
  "archived":{"type":"boolean","description":"Include archived cards too"}`, "board"),
				Result: object(`"cards":{"type":"array","items":`+cardSchema+`,"description":"The page's cards"},
  "cursor":{"type":"string","description":"The next page's cursor; empty on the last page"}`, "cards"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"board":"b1","limit":100}`), Result: json.RawMessage(`{"cards":[` + cardExample + `],"cursor":""}`)}},
				Errors:   reads,
			},
			{
				ID: "card.get", Title: "Get a card", Description: "Reads one card with its version and updatedAt. notFound when the tracker has no such card.",
				Effect: connectors.EffectRead, Args: object(idArg("card"), "id"), Result: json.RawMessage(cardSchema),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"c1"}`), Result: json.RawMessage(cardExample)}},
				Errors:   reads,
			},
			{
				ID: "card.create", Title: "Create a card", Description: "Makes a card in a list, keyed by our task id: a create with a key already made makes nothing and gives that card.",
				Effect: connectors.EffectWrite, Key: "key",
				Args: object(`"key":{"type":"string","description":"Our task id, the idempotency key"},
  "board":{"type":"string","description":"The board's id"},
  "list":{"type":"string","description":"The list's id (board.get)"},
  "position":{"type":"number","description":"Where in the list: a position between its neighbours'; the end when left out"},
  `+fields, "key", "board", "list", "title"),
				Result:   json.RawMessage(cardSchema),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"key":"7340032001","board":"b1","list":"l2","title":"Write the launch post","description":"Draft and review","assignees":["m1"],"due":"2026-12-01"}`), Result: json.RawMessage(cardExample)}},
				Errors:   writes,
			},
			{
				ID: "card.update", Title: "Change a card", Description: "Sets some of a card's fields; fields left out of set are kept. Given version, it is refused with conflict when the card changed since that version, so a sync never overwrites a change it has not seen.",
				Effect: connectors.EffectWrite,
				Args: object(idArg("card")+`,
  "version":{"type":"string","description":"The version the change was made against; refused with conflict once the card moved on"},
  "set":{"type":"object","description":"The fields to change","properties":{`+fields+`}}`, "id", "set"),
				Result:   json.RawMessage(cardSchema),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"c1","version":"2","set":{"title":"Write the launch post"}}`), Result: json.RawMessage(cardExample)}},
				Errors:   append(writes, connectors.CodeConflict),
			},
			{
				ID: "card.move", Title: "Move a card", Description: "Moves a card to a list (status), at a position among its cards; the end when position is left out. Given version, refused with conflict once the card moved on.",
				Effect: connectors.EffectWrite,
				Args: object(idArg("card")+`,
  "list":{"type":"string","description":"The list's id"},
  "position":{"type":"number","description":"A position between its new neighbours'; the end when left out"},
  "version":{"type":"string","description":"The version the move was made against"}`, "id", "list"),
				Result:   json.RawMessage(cardSchema),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"c1","list":"l2","position":1024}`), Result: json.RawMessage(cardExample)}},
				Errors:   append(writes, connectors.CodeConflict),
			},
			{
				ID: "card.archive", Title: "Archive a card", Description: "Archives (closes) a card; archiving an archived card gives it unchanged.",
				Effect: connectors.EffectWrite, Destructive: true, Args: object(idArg("card"), "id"), Result: json.RawMessage(cardSchema),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"c1"}`), Result: json.RawMessage(cardExample)}},
				Errors:   writes,
			},
			{
				ID: "comment.list", Title: "List comments", Description: "Lists a card's comments, oldest first.",
				Effect: connectors.EffectRead, Args: object(`"card":{"type":"string","description":"The card's id"}`, "card"),
				Result:   object(`"comments":{"type":"array","items":`+commentSchema+`,"description":"The comments, oldest first"}`, "comments"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"card":"c1"}`), Result: json.RawMessage(`{"comments":[` + commentExample + `]}`)}},
				Errors:   reads,
			},
			{
				ID: "comment.create", Title: "Comment on a card", Description: "Adds a comment to a card, keyed by our comment id: a create with a key already made gives that comment.",
				Effect: connectors.EffectWrite, Key: "key",
				Args: object(`"key":{"type":"string","description":"Our comment id, the idempotency key"},
  "card":{"type":"string","description":"The card's id"},
  "text":{"type":"string","description":"The comment, in Markdown"}`, "key", "card", "text"),
				Result:   json.RawMessage(commentSchema),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"key":"7340032101","card":"c1","text":"Looks good"}`), Result: json.RawMessage(commentExample)}},
				Errors:   writes,
			},
			{
				ID: "member.list", Title: "List members", Description: "Lists the members of a board, to map them to Axiom's users by email or name.",
				Effect: connectors.EffectRead, Args: object(`"board":{"type":"string","description":"The board's id"}`, "board"),
				Result:   object(`"members":{"type":"array","items":`+memberSchema+`,"description":"The members"}`, "members"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"board":"b1"}`), Result: json.RawMessage(`{"members":[{"id":"m1","name":"Ada Lovelace","username":"ada"}]}`)}},
				Errors:   reads,
			},
			{
				ID: "check", Title: "Check", Description: "Signs in and changes nothing: whether the connection's credentials work.",
				Effect: connectors.EffectRead, Args: object(""),
				Result:   object(`"ok":{"type":"boolean","description":"Always true: a failure is an error"}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   []string{connectors.CodeAuth, connectors.CodeUnavailable},
			},
		},
		Events: []connectors.EventDef{
			{
				ID: "card.changed", Title: "Card changed", Description: "A card was created, changed, moved or archived in the tracker. It carries the card's version after the change, so a sync drops the echo of its own change (the version it already holds) and reads the card again for the rest.",
				Payload: object(`"id":{"type":"string","description":"The event's own id, unique"},
  "card":{"type":"string","description":"The card's outside id"},
  "board":{"type":"string","description":"Its board's id"},
  "change":{"type":"string","enum":["created","updated","moved","archived"],"description":"What happened"},
  "version":{"type":"string","description":"The card's version after the change"},
  "updatedAt":{"type":"string","description":"When, RFC 3339, by the tracker's clock"},
  "fields":{"type":"array","items":{"type":"string"},"description":"The fields that changed, when the tracker says"}`, "id", "card", "board", "change", "version"),
				Example:      json.RawMessage(`{"id":"evt_1","card":"c1","board":"b1","change":"moved","version":"4","updatedAt":"2026-11-27T10:00:00Z","fields":["list","position"]}`),
				IDFields:     []string{"id"},
				Delivery:     []string{connectors.DeliveryWatch},
				Subscription: "card.changed",
			},
			{
				ID: "comment.created", Title: "Comment created", Description: "Someone commented on a card in the tracker.",
				Payload: object(`"id":{"type":"string","description":"The event's own id, unique"},
  "card":{"type":"string","description":"The card's outside id"},
  "board":{"type":"string","description":"Its board's id"},
  "comment":{"type":"string","description":"The comment's outside id"},
  "version":{"type":"string","description":"The card's version after the comment"},
  "createdAt":{"type":"string","description":"When, RFC 3339"}`, "id", "card", "board", "comment"),
				Example:      json.RawMessage(`{"id":"evt_2","card":"c1","board":"b1","comment":"cm1","version":"5","createdAt":"2026-11-27T10:05:00Z"}`),
				IDFields:     []string{"id"},
				Delivery:     []string{connectors.DeliveryWatch},
				Subscription: "comment.created",
			},
		},
	}
}

// Contribute registers tasks.tracker@1 as a builtin contract.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, "tasks", []connectors.Contract{Contract()}, nil)
}
