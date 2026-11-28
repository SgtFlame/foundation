// Package messaging is the contract for outside chat services (forge
// 26f-1): chat.messaging@1, which Slack, Discord, Teams and the Messaging
// test server implement, so the Nexus bridges (26f-2) are the same for
// every service. The test server is messaging/msgtest and the suite every
// type passes is messaging/contracttest.
package messaging

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// The contract's reference.
const (
	ContractID  = "chat.messaging"
	ContractRef = "chat.messaging@1"
)

// CodeCursorExpired is the code an expired cursor's invalid error carries in
// its message: the caller lists again without one.
const CodeCursorExpired = "cursor_expired"

// Channel kinds.
const (
	KindChannel = "channel" // public in its workspace
	KindPrivate = "private" // members only
	KindDirect  = "direct"  // two people
	KindGroup   = "group"   // a few people without a name
)

// Channel is a channel as chat.messaging@1 carries it.
type Channel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Topic    string `json:"topic,omitempty"`
	Members  int    `json:"members,omitempty"`
	Archived bool   `json:"archived,omitempty"`
}

// Member is a person (or bot) of the service.
type Member struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Bot   bool   `json:"bot,omitempty"`
}

// Author is who posted a message.
type Author struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Bot  bool   `json:"bot,omitempty"`
}

// File is a file attached to a message.
type File struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	URL         string `json:"url,omitempty"`
}

// Reaction is one emoji's reactions on a message.
type Reaction struct {
	Emoji string   `json:"emoji"`
	Count int      `json:"count"`
	Users []string `json:"users,omitempty"`
}

// Message is a message as chat.messaging@1 carries it.
type Message struct {
	ID        string     `json:"id"`
	Channel   string     `json:"channel"`
	Thread    string     `json:"thread,omitempty"`
	Key       string     `json:"key,omitempty"`
	Author    Author     `json:"author"`
	Text      string     `json:"text"`
	Mentions  []string   `json:"mentions,omitempty"`
	Files     []File     `json:"files,omitempty"`
	Reactions []Reaction `json:"reactions,omitempty"`
	Replies   int        `json:"replies,omitempty"`
	Posted    string     `json:"posted"`
	Edited    string     `json:"edited,omitempty"`
}

const source = "messaging"

// ChannelSchema is the channel shape's JSON Schema.
const ChannelSchema = `{"type":"object","description":"A channel","properties":{
  "id":{"type":"string","description":"The channel's id at the service"},
  "name":{"type":"string","description":"Its name, without a leading #; for a direct conversation the other person's name"},
  "kind":{"type":"string","enum":["channel","private","direct","group"],"description":"channel is public in its workspace, private for members only, direct between two people, group a few people without a name"},
  "topic":{"type":"string","description":"Its topic"},
  "members":{"type":"integer","description":"How many members it has"},
  "archived":{"type":"boolean","description":"Whether it is archived (read-only)"}},"required":["id","name","kind"]}`

// MemberSchema is the member shape's JSON Schema.
const MemberSchema = `{"type":"object","description":"A person or bot of the service","properties":{
  "id":{"type":"string","description":"Their id at the service"},
  "name":{"type":"string","description":"Their display name"},
  "email":{"type":"string","description":"Their email, when the service shares it"},
  "bot":{"type":"boolean","description":"Whether it is a bot"}},"required":["id","name"]}`

const fileSchema = `{"type":"object","description":"A file attached to a message","properties":{
  "id":{"type":"string","description":"The file's id at the service"},
  "name":{"type":"string","description":"Its name"},
  "size":{"type":"integer","description":"Its size in bytes"},
  "contentType":{"type":"string","description":"Its media type"},
  "url":{"type":"string","description":"Where the service serves it to the connection"}},"required":["id","name"]}`

// MessageSchema is the message shape's JSON Schema.
const MessageSchema = `{"type":"object","description":"A message","properties":{
  "id":{"type":"string","description":"The message's id at the service"},
  "channel":{"type":"string","description":"Its channel's id"},
  "thread":{"type":"string","description":"For a reply: the id of the message that starts its thread"},
  "key":{"type":"string","description":"The key it was posted with through this contract, when it was; bridges know their own posts by it"},
  "author":{"type":"object","description":"Who posted it","properties":{"id":{"type":"string","description":"Their id"},"name":{"type":"string","description":"Their name"},"bot":{"type":"boolean","description":"Whether a bot posted it"}},"required":["id"]},
  "text":{"type":"string","description":"Its text as Markdown, mentions as @<member id>"},
  "mentions":{"type":"array","items":{"type":"string"},"description":"The member ids it mentions"},
  "files":{"type":"array","items":` + fileSchema + `,"description":"Its files"},
  "reactions":{"type":"array","description":"Its reactions, one per emoji","items":{"type":"object","properties":{"emoji":{"type":"string","description":"The emoji, e.g. thumbsup"},"count":{"type":"integer","description":"How many reacted"},"users":{"type":"array","items":{"type":"string"},"description":"Who reacted"}},"required":["emoji","count"]}},
  "replies":{"type":"integer","description":"How many replies its thread has"},
  "posted":{"type":"string","description":"When it was posted, RFC 3339"},
  "edited":{"type":"string","description":"When it was last edited, RFC 3339"}},"required":["id","channel","author","text","posted"]}`

// MessageExample is an example message.
const MessageExample = `{"id":"m-1","channel":"general","author":{"id":"u-dana","name":"Dana"},"text":"Standup moved to 10:30","posted":"2026-12-01T09:00:00Z"}`

func obj(props string, required ...string) json.RawMessage {
	r := ""
	if len(required) > 0 {
		b, _ := json.Marshal(required)
		r = `,"required":` + string(b)
	}
	return json.RawMessage(`{"type":"object","properties":{` + props + `}` + r + `}`)
}

// Contract is chat.messaging@1.
func Contract() connectors.Contract {
	retryable := []string{connectors.CodeRateLimited, connectors.CodeUnavailable}
	message := json.RawMessage(MessageSchema)
	ok := obj(`"ok":{"type":"boolean","description":"Always true: a failure is an error"}`, "ok")
	ref := `"channel":{"type":"string","description":"The channel's id"},"id":{"type":"string","description":"The message's id"}`
	notice := `"id":{"type":"string","description":"The notice's own id, unique"},"channel":{"type":"string","description":"The channel's id"}`
	both := []string{connectors.DeliveryWatch, connectors.DeliveryCallback}
	return connectors.Contract{
		ID: ContractID, Version: 1,
		Meta: connectors.Meta{Title: "Chat messaging", Description: "Reads and posts in an outside chat service (Slack, Discord, Microsoft Teams): its channels and members, messages in a channel or a thread or the changes since a cursor, posting (keyed, so a retry posts once), editing and deleting messages, reactions and file uploads; and raises an event for each message posted, edited or deleted, each reaction and each channel change. Every chat service implements it, so bridging a Nexus channel is the same for every service.", Tags: []string{"chat", "messaging", "channels", "bridge"}},
		Operations: []connectors.Operation{
			{
				ID: "channels.list", Title: "List channels", Description: "The channels the connection's account can see or is in, with their kind (channel, private, direct, group), topic and member count.",
				Effect: connectors.EffectRead, Args: obj(``),
				Result:   obj(`"channels":{"type":"array","items":`+ChannelSchema+`,"description":"The channels"}`, "channels"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"channels":[{"id":"general","name":"general","kind":"channel","members":3}]}`)}},
				Errors:   append([]string{connectors.CodeAuth}, retryable...),
			},
			{
				ID: "members.list", Title: "List members", Description: "The members of a channel, or of the whole workspace when no channel is given.",
				Effect:   connectors.EffectRead,
				Args:     obj(`"channel":{"type":"string","description":"The channel's id; the whole workspace when left out"}`),
				Result:   obj(`"members":{"type":"array","items":`+MemberSchema+`,"description":"The members"}`, "members"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general"}`), Result: json.RawMessage(`{"members":[{"id":"u-dana","name":"Dana"}]}`)}},
				Errors:   append([]string{connectors.CodeNotFound, connectors.CodeAuth}, retryable...),
			},
			{
				ID: "messages.list", Title: "List messages", Description: "The latest messages of a channel (or of one thread), oldest first, or with since the messages posted or edited after that cursor and the ids of those deleted. Either way it gives the cursor to store. A cursor too old is refused as invalid with cursor_expired in its message: list again without one.",
				Effect: connectors.EffectRead,
				Args: obj(`"channel":{"type":"string","description":"The channel's id"},
  "thread":{"type":"string","description":"A thread's first message id: only that thread's replies"},
  "since":{"type":"string","description":"The cursor from the last list, to get only changes since"},
  "limit":{"type":"integer","minimum":1,"maximum":200,"description":"At most this many messages; 50 when left out"}`, "channel"),
				Result: obj(`"messages":{"type":"array","items":`+MessageSchema+`,"description":"The messages, oldest first"},
  "deleted":{"type":"array","items":{"type":"string"},"description":"Ids of messages deleted since the cursor"},
  "cursor":{"type":"string","description":"The cursor to store for the next list"}`, "messages", "cursor"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general","limit":20}`), Result: json.RawMessage(`{"messages":[` + MessageExample + `],"cursor":"4"}`)}},
				Errors:   append([]string{connectors.CodeNotFound, connectors.CodeInvalid, connectors.CodeAuth}, retryable...),
			},
			{
				ID: "message.post", Title: "Post a message", Description: "Posts a message in a channel, or as a reply in a thread, as the connection's account, under our key (a post with a key already used posts nothing and gives that message). Mention a member as @<member id>; attach files uploaded with file.upload by their ids.",
				Effect: connectors.EffectSend, Key: "key",
				Args: obj(`"channel":{"type":"string","description":"The channel's id"},
  "key":{"type":"string","description":"Our id for the message, the idempotency key"},
  "text":{"type":"string","description":"The text as Markdown"},
  "thread":{"type":"string","description":"Reply in the thread this message starts"},
  "files":{"type":"array","items":{"type":"string"},"description":"Ids of files uploaded with file.upload"}`, "channel", "key", "text"),
				Result:   message,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general","key":"nx-123","text":"Standup moved to 10:30"}`), Result: json.RawMessage(MessageExample)}},
				Errors:   append([]string{connectors.CodeNotFound, connectors.CodeInvalid, connectors.CodeAuth}, retryable...),
			},
			{
				ID: "message.edit", Title: "Edit a message", Description: "Replaces the text of a message the connection's account posted. Editing someone else's message is refused as invalid.",
				Effect: connectors.EffectSend,
				Args: obj(ref+`,
  "text":{"type":"string","description":"The new text as Markdown"}`, "channel", "id", "text"),
				Result:   message,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general","id":"m-1","text":"Standup moved to 10:45"}`), Result: json.RawMessage(`{"id":"m-1","channel":"general","author":{"id":"u-dana","name":"Dana"},"text":"Standup moved to 10:45","posted":"2026-12-01T09:00:00Z","edited":"2026-12-01T09:05:00Z"}`)}},
				Errors:   append([]string{connectors.CodeNotFound, connectors.CodeInvalid}, retryable...),
			},
			{
				ID: "message.delete", Title: "Delete a message", Description: "Deletes a message the connection's account posted. Deleting one that is gone succeeds, with deleted false.",
				Effect: connectors.EffectSend, Destructive: true,
				Args:     obj(ref, "channel", "id"),
				Result:   obj(`"deleted":{"type":"boolean","description":"Whether there was a message to delete"}`, "deleted"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general","id":"m-1"}`), Result: json.RawMessage(`{"deleted":true}`)}},
				Errors:   append([]string{connectors.CodeInvalid}, retryable...),
			},
			{
				ID: "reaction.add", Title: "Add a reaction", Description: "Reacts to a message with an emoji as the connection's account. Adding one already there succeeds.",
				Effect:   connectors.EffectSend,
				Args:     obj(ref+`,"emoji":{"type":"string","description":"The emoji's name, e.g. thumbsup"}`, "channel", "id", "emoji"),
				Result:   ok,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general","id":"m-1","emoji":"thumbsup"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   append([]string{connectors.CodeNotFound, connectors.CodeInvalid}, retryable...),
			},
			{
				ID: "reaction.remove", Title: "Remove a reaction", Description: "Takes back the connection's account's reaction. Removing one not there succeeds.",
				Effect:   connectors.EffectSend,
				Args:     obj(ref+`,"emoji":{"type":"string","description":"The emoji's name"}`, "channel", "id", "emoji"),
				Result:   ok,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general","id":"m-1","emoji":"thumbsup"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   append([]string{connectors.CodeNotFound}, retryable...),
			},
			{
				ID: "file.upload", Title: "Upload a file", Description: "Uploads a file to attach to a message posted after (message.post's files), its content base64 encoded. Gives the file with its id.",
				Effect: connectors.EffectWrite,
				Args: obj(`"channel":{"type":"string","description":"The channel it is for"},
  "name":{"type":"string","description":"The file's name"},
  "contentType":{"type":"string","description":"Its media type"},
  "data":{"type":"string","description":"Its content, base64 encoded"}`, "channel", "name", "data"),
				Result:   json.RawMessage(fileSchema),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"channel":"general","name":"notes.txt","contentType":"text/plain","data":"aGVsbG8="}`), Result: json.RawMessage(`{"id":"f-1","name":"notes.txt","size":5,"contentType":"text/plain"}`)}},
				Errors:   append([]string{connectors.CodeNotFound, connectors.CodeInvalid}, retryable...),
			},
			{
				ID: "check", Title: "Check", Description: "Signs in and lists the channels: whether the connection's credentials work.",
				Effect: connectors.EffectRead, Args: obj(``), Result: ok,
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   []string{connectors.CodeAuth, connectors.CodeUnavailable},
			},
		},
		Events: []connectors.EventDef{
			{
				ID: "messagePosted", Title: "Message posted", Description: "Someone posted a message, the connection's own account too (a bridge knows its own posts by key).",
				Payload:  obj(notice+`,"message":`+MessageSchema, "id", "channel", "message"),
				Example:  json.RawMessage(`{"id":"n-1","channel":"general","message":` + MessageExample + `}`),
				IDFields: []string{"id"}, Delivery: both, Subscription: "messagePosted",
			},
			{
				ID: "messageEdited", Title: "Message edited", Description: "A message's text changed.",
				Payload:  obj(notice+`,"message":`+MessageSchema, "id", "channel", "message"),
				Example:  json.RawMessage(`{"id":"n-2","channel":"general","message":` + MessageExample + `}`),
				IDFields: []string{"id"}, Delivery: both, Subscription: "messageEdited",
			},
			{
				ID: "messageDeleted", Title: "Message deleted", Description: "A message was deleted.",
				Payload:  obj(notice+`,"message":{"type":"string","description":"The deleted message's id"}`, "id", "channel", "message"),
				Example:  json.RawMessage(`{"id":"n-3","channel":"general","message":"m-1"}`),
				IDFields: []string{"id"}, Delivery: both, Subscription: "messageDeleted",
			},
			{
				ID: "reactionChanged", Title: "Reaction changed", Description: "Someone added or took back a reaction.",
				Payload: obj(notice+`,"message":{"type":"string","description":"The message's id"},
  "emoji":{"type":"string","description":"The emoji's name"},
  "user":{"type":"string","description":"Who reacted"},
  "added":{"type":"boolean","description":"true when added, false when taken back"}`, "id", "channel", "message", "emoji", "user", "added"),
				Example:  json.RawMessage(`{"id":"n-4","channel":"general","message":"m-1","emoji":"thumbsup","user":"u-lee","added":true}`),
				IDFields: []string{"id"}, Delivery: both, Subscription: "reactionChanged",
			},
			{
				ID: "channelChanged", Title: "Channel changed", Description: "A channel was made, renamed, archived or had its members change: list the channels again.",
				Payload:  obj(notice, "id", "channel"),
				Example:  json.RawMessage(`{"id":"n-5","channel":"general"}`),
				IDFields: []string{"id"}, Delivery: both, Subscription: "channelChanged",
			},
		},
	}
}

// Contribute registers chat.messaging@1.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, source, []connectors.Contract{Contract()}, nil)
}
