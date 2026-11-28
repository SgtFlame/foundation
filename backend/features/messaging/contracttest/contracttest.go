// Package contracttest is the suite every chat.messaging@1 type passes
// (forge 26f-1): the Messaging test server here, Slack, Discord and Teams
// against recorded responses in 26g to 26i.
package contracttest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/messaging"
)

// Caller calls one chat.messaging operation of the type under test.
type Caller func(op string, args any) (json.RawMessage, error)

// Options say what the suite may use.
type Options struct {
	// Channel is a channel the account may post in; the first public one
	// listed when empty.
	Channel string
	// Self is the account's member id: what it posts is its.
	Self string
	// Expire makes every cursor given so far too old (the test server's
	// expireCursor); nil skips that step.
	Expire func() error
	// NoFiles skips the upload step, for a recording without it.
	NoFiles bool
}

type listed struct {
	Messages []messaging.Message `json:"messages"`
	Deleted  []string            `json:"deleted"`
	Cursor   string              `json:"cursor"`
}

// Run runs the suite: a post is keyed; the channel lists what was posted,
// a thread's replies only under their thread; the cursor gives only the
// changes since, deletions as ids, and an expired one is reported as
// cursor_expired; edit changes the text; reactions add and remove once;
// delete is idempotent; uploaded files attach to a post.
func Run(t *testing.T, call Caller, o Options) {
	t.Helper()
	must := func(op string, args any, into any) {
		t.Helper()
		raw, err := call(op, args)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if into != nil {
			if err := json.Unmarshal(raw, into); err != nil {
				t.Fatalf("%s: %v in %s", op, err, raw)
			}
		}
	}
	code := func(err error) string {
		var ce *connectors.Error
		if errors.As(err, &ce) {
			return ce.Code
		}
		return ""
	}
	if _, err := call("check", map[string]any{}); err != nil {
		t.Fatalf("check: %v", err)
	}
	var chans struct{ Channels []messaging.Channel }
	must("channels.list", map[string]any{}, &chans)
	ch := o.Channel
	for _, c := range chans.Channels {
		if ch == "" && c.Kind == messaging.KindChannel && !c.Archived {
			ch = c.ID
		}
	}
	if ch == "" {
		t.Fatalf("channels.list has no public channel: %+v", chans.Channels)
	}
	var members struct{ Members []messaging.Member }
	must("members.list", map[string]any{"channel": ch}, &members)
	if o.Self != "" {
		found := false
		for _, m := range members.Members {
			found = found || m.ID == o.Self
		}
		if !found {
			t.Errorf("members.list of %s leaves out the account %s: %+v", ch, o.Self, members.Members)
		}
	}
	if _, err := call("messages.list", map[string]any{"channel": "no-such-channel"}); code(err) != connectors.CodeNotFound {
		t.Errorf("listing a missing channel: %v, want notFound", err)
	}

	var start listed
	must("messages.list", map[string]any{"channel": ch}, &start)

	// A post is keyed: posting again with its key gives the same message.
	var first, again messaging.Message
	must("message.post", map[string]any{"channel": ch, "key": "suite-1", "text": "Hello from the suite"}, &first)
	if first.ID == "" || first.Channel != ch || first.Text != "Hello from the suite" || first.Posted == "" {
		t.Fatalf("message.post gave %+v", first)
	}
	if o.Self != "" && first.Author.ID != o.Self {
		t.Errorf("the post's author is %q, want the account %q", first.Author.ID, o.Self)
	}
	must("message.post", map[string]any{"channel": ch, "key": "suite-1", "text": "Hello from the suite"}, &again)
	if again.ID != first.ID {
		t.Errorf("posting with the same key made a second message: %s and %s", first.ID, again.ID)
	}

	// A reply lives under its thread, not in the channel's own list.
	var reply messaging.Message
	must("message.post", map[string]any{"channel": ch, "key": "suite-2", "text": "A reply", "thread": first.ID}, &reply)
	if reply.Thread != first.ID {
		t.Errorf("a reply's thread is %q, want %q", reply.Thread, first.ID)
	}
	var top, thread listed
	must("messages.list", map[string]any{"channel": ch}, &top)
	must("messages.list", map[string]any{"channel": ch, "thread": first.ID}, &thread)
	if !has(top.Messages, first.ID) || has(top.Messages, reply.ID) {
		t.Errorf("the channel's list should hold the post and not its reply: %+v", ids(top.Messages))
	}
	if len(thread.Messages) != 1 || thread.Messages[0].ID != reply.ID {
		t.Errorf("the thread's list should hold only the reply: %+v", ids(thread.Messages))
	}
	if n := len(top.Messages); n > 0 && top.Messages[n-1].ID != first.ID {
		t.Errorf("messages are listed oldest first: the newest top-level post %s is not last in %v", first.ID, ids(top.Messages))
	}

	// Edit, then the cursor gives only what changed since.
	var changes listed
	must("messages.list", map[string]any{"channel": ch, "since": top.Cursor}, &changes)
	if len(changes.Messages) != 0 || len(changes.Deleted) != 0 {
		t.Errorf("nothing changed since the cursor, yet %+v", changes)
	}
	var edited messaging.Message
	must("message.edit", map[string]any{"channel": ch, "id": first.ID, "text": "Hello again"}, &edited)
	if edited.Text != "Hello again" || edited.Edited == "" {
		t.Errorf("message.edit gave %+v", edited)
	}
	must("messages.list", map[string]any{"channel": ch, "since": top.Cursor}, &changes)
	if len(changes.Messages) != 1 || changes.Messages[0].ID != first.ID || changes.Messages[0].Text != "Hello again" {
		t.Errorf("the changes since the cursor should be the edit: %+v", changes)
	}

	// Reactions add once and remove once.
	for _, op := range []string{"reaction.add", "reaction.add"} {
		must(op, map[string]any{"channel": ch, "id": first.ID, "emoji": "thumbsup"}, nil)
	}
	var withReaction listed
	must("messages.list", map[string]any{"channel": ch, "since": changes.Cursor}, &withReaction)
	if len(withReaction.Messages) != 1 || reactions(withReaction.Messages[0], "thumbsup") != 1 {
		t.Errorf("one account reacting twice should count once: %+v", withReaction.Messages)
	}
	for _, op := range []string{"reaction.remove", "reaction.remove"} {
		must(op, map[string]any{"channel": ch, "id": first.ID, "emoji": "thumbsup"}, nil)
	}
	var noReaction listed
	must("messages.list", map[string]any{"channel": ch, "thread": "", "since": withReaction.Cursor}, &noReaction)
	if len(noReaction.Messages) == 1 && reactions(noReaction.Messages[0], "thumbsup") != 0 {
		t.Errorf("the reaction should be gone: %+v", noReaction.Messages)
	}

	// Files attach to a post.
	if !o.NoFiles {
		var f messaging.File
		must("file.upload", map[string]any{"channel": ch, "name": "notes.txt", "contentType": "text/plain", "data": base64.StdEncoding.EncodeToString([]byte("hello"))}, &f)
		if f.ID == "" || f.Name != "notes.txt" {
			t.Errorf("file.upload gave %+v", f)
		}
		var withFile messaging.Message
		must("message.post", map[string]any{"channel": ch, "key": "suite-3", "text": "See the notes", "files": []string{f.ID}}, &withFile)
		if len(withFile.Files) != 1 || withFile.Files[0].ID != f.ID {
			t.Errorf("the post should carry the file: %+v", withFile.Files)
		}
	}

	// Delete is idempotent, and a cursor reports it.
	var del struct{ Deleted bool }
	must("message.delete", map[string]any{"channel": ch, "id": reply.ID}, &del)
	if !del.Deleted {
		t.Error("deleting the reply said there was nothing to delete")
	}
	must("message.delete", map[string]any{"channel": ch, "id": reply.ID}, &del)
	if del.Deleted {
		t.Error("deleting the reply twice said it deleted it again")
	}
	var afterDelete listed
	must("messages.list", map[string]any{"channel": ch, "thread": first.ID, "since": thread.Cursor}, &afterDelete)
	if !contains(afterDelete.Deleted, reply.ID) || has(afterDelete.Messages, reply.ID) {
		t.Errorf("the changes since should report the reply deleted: %+v", afterDelete)
	}

	if o.Expire != nil {
		if err := o.Expire(); err != nil {
			t.Fatalf("expire: %v", err)
		}
		_, err := call("messages.list", map[string]any{"channel": ch, "since": afterDelete.Cursor})
		if code(err) != connectors.CodeInvalid || !strings.Contains(err.Error(), messaging.CodeCursorExpired) {
			t.Errorf("an expired cursor gave %v, want invalid with %s", err, messaging.CodeCursorExpired)
		}
		var fresh listed
		must("messages.list", map[string]any{"channel": ch}, &fresh)
		if fresh.Cursor == "" {
			t.Error("listing again without a cursor gave no cursor")
		}
	}
}

func has(ms []messaging.Message, id string) bool {
	for _, m := range ms {
		if m.ID == id {
			return true
		}
	}
	return false
}

func ids(ms []messaging.Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func reactions(m messaging.Message, emoji string) int {
	for _, r := range m.Reactions {
		if r.Emoji == emoji {
			return r.Count
		}
	}
	return 0
}
