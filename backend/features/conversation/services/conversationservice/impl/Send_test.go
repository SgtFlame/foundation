package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// fakeChat replies with the words of the last message, and "A title" to
// title calls; block holds a reply until the caller cancels.
type fakeChat struct {
	got   []chatservice.CompleteArgs
	block bool
}

func (f *fakeChat) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	f.got = append(f.got, args)
	return chatservice.CompleteResponse{Common: features.OK(), Message: chatservice.Message{Role: chatservice.RoleAssistant, Content: `"A title."`}}
}

func (f *fakeChat) Stream(args chatservice.StreamArgs) *chatservice.StreamRequest {
	f.got = append(f.got, args)
	deltas := make(chan chatservice.Delta, 8)
	resp := make(chan *chatservice.CompleteResponse, 1)
	go func() {
		deltas <- chatservice.Delta{Text: "re: "}
		if f.block {
			<-args.Context().Done()
			close(deltas)
			resp <- &chatservice.CompleteResponse{Common: features.ServiceResponse{Code: http.StatusGatewayTimeout}}
			return
		}
		text := args.Messages[len(args.Messages)-1].Content
		deltas <- chatservice.Delta{Text: text}
		close(deltas)
		resp <- &chatservice.CompleteResponse{
			Common: features.OK(), Model: "m-flash", Level: args.Level, FinishReason: "stop",
			Message: chatservice.Message{Role: chatservice.RoleAssistant, Content: "re: " + text},
			Usage:   chatservice.Usage{InputTokens: 1000, OutputTokens: 100},
		}
	}()
	return &chatservice.StreamRequest{Deltas: deltas, Response: resp}
}

type sections map[string]string

func (s sections) Resolved(_ features.ServiceArgs, section string) (json.RawMessage, error) {
	if v, ok := s[section]; ok {
		return json.RawMessage(v), nil
	}
	return nil, context.Canceled // any error: not found
}

func newTestService(t *testing.T, chat *fakeChat) (*service, *[]conversation.GatherArgs) {
	t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, slices.Concat(conversation.Models(), retrieval.Models(), tools.Models())...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	var gathered []conversation.GatherArgs
	conversation.ContextProviders.Register(reg, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: "test"},
		Title:      "Test",
		Defaults:   json.RawMessage(`{"enabled": true}`),
		Gather: func(args conversation.GatherArgs) ([]conversation.ContextItem, error) {
			gathered = append(gathered, args)
			return []conversation.ContextItem{{Title: "Workspace", Text: string(args.Workbench), Score: 1}}, nil
		},
	})
	config := sections{
		conversationsettings.ID: `{"systemPrompt": "Be helpful.", "historyRounds": 1, "titleLevel": "fast"}`,
		aisettings.LevelsID:     `{"levels": [{"id": "fast", "modelRef": "lite"}, {"id": "standard", "modelRef": "flash", "default": true}]}`,
		aisettings.ModelsID: `{"models": [{"id": "lite", "model": "m-lite"},
			{"id": "flash", "model": "m-flash", "pricing": {"inputPerMTok": 1, "outputPerMTok": 10}}]}`,
	}
	return newService(WithDB(gdb), WithChat(chat), WithConfig(config), WithRegistry(reg)), &gathered
}

func collect(req *conversationservice.SendRequest) map[conversationservice.SendEventKind][]conversationservice.SendEvent {
	out := map[conversationservice.SendEventKind][]conversationservice.SendEvent{}
	for e := range req.Events {
		out[e.Kind] = append(out[e.Kind], e)
	}
	return out
}

func TestSendStoresAndStreamsRounds(t *testing.T) {
	chat := &fakeChat{}
	s, gathered := newTestService(t, chat)
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}

	first := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: " hello ", Workbench: json.RawMessage(`{"surface":"main"}`)}))
	if len(first[conversationservice.EventStarted]) != 1 || len(first[conversationservice.EventDelta]) != 2 {
		t.Fatalf("events %+v", first)
	}
	done := first[conversationservice.EventDone][0].Round
	if done.Status != http.StatusOK || done.Reply != "re: hello" || done.Level != "standard" || done.ModelName != "m-flash" ||
		done.CostMicros != 1000*1+100*10 || done.Currency != "USD" || done.Workbench != `{"surface":"main"}` {
		t.Errorf("done round %+v", done)
	}
	title := first[conversationservice.EventTitle]
	if len(title) != 1 || title[0].Conversation.Title != "A title" {
		t.Errorf("title %+v", title)
	}
	call := chat.got[0]
	if call.Level != "standard" || call.Meta.Feature != "chat" || call.Meta.RoundID != done.ID || call.Meta.ConversationID != done.ConversationID ||
		!strings.HasPrefix(call.System, "Be helpful.") || !strings.Contains(call.System, `## Workspace`+"\n"+`{"surface":"main"}`) {
		t.Errorf("chat call %+v", call)
	}
	if chat.got[1].Level != "fast" || chat.got[1].Meta.Feature != "chat-title" {
		t.Errorf("title call %+v", chat.got[1])
	}

	// The next round sends one round of history and, with the provider
	// turned off for the conversation, no context
	id := done.ConversationID
	collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: id, Text: "again", Level: "fast",
		ContextSettings: map[string]json.RawMessage{"test": json.RawMessage(`{"enabled": false}`)}}))
	third := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: id, Text: "third"}))
	msgs := chat.got[len(chat.got)-1].Messages
	if len(msgs) != 3 || msgs[0].Content != "again" || msgs[1].Content != "re: again" || len(*gathered) != 1 {
		t.Errorf("history %+v, gathered %d", msgs, len(*gathered))
	}
	if len(third[conversationservice.EventTitle]) != 0 {
		t.Errorf("a titled conversation is titled again")
	}

	list := s.List(conversationservice.ListArgs{ServiceArgs: args})
	rounds := s.Rounds(conversationservice.RoundsArgs{ServiceArgs: args, ConversationID: id})
	if len(list.Conversations) != 1 || list.Conversations[0].Title != "A title" || len(rounds.Rounds) != 3 || rounds.Rounds[0].Text != "hello" {
		t.Errorf("list %+v, rounds %d", list, len(rounds.Rounds))
	}

	// Another user's conversation is not found
	other := args
	other.Principal.UserID = 99
	if code := s.Rounds(conversationservice.RoundsArgs{ServiceArgs: other, ConversationID: id}).Common.Code; code != http.StatusNotFound {
		t.Errorf("other user: code %d", code)
	}
	if code := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "x", Level: "nope"}))[conversationservice.EventFailed][0].Common.Code; code != http.StatusBadRequest {
		t.Errorf("unknown level: code %d", code)
	}
}

func TestStoppedRoundsKeepTheirReply(t *testing.T) {
	s, _ := newTestService(t, &fakeChat{block: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := s.Send(conversationservice.SendArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}, Text: "long"})
	for e := range req.Events {
		if e.Kind == conversationservice.EventDelta {
			cancel()
		}
	}
	var r conversation.Round
	if err := s.db.First(&r).Error; err != nil {
		t.Fatal(err)
	}
	if r.Status != conversation.StatusStopped || r.Reply != "re: " || r.FinishReason != "stopped" || !r.InHistory() {
		t.Errorf("stopped round %+v", r)
	}
}

func TestRoundsCiteSegmentsAndTakeFeedback(t *testing.T) {
	chat := &fakeChat{}
	s, _ := newTestService(t, chat)
	conversation.ContextProviders.Register(s.registry, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: "segments"},
		Gather: func(args conversation.GatherArgs) ([]conversation.ContextItem, error) {
			var items []conversation.ContextItem
			for i, key := range []string{"k1", "k2"} {
				seg := retrieval.Scored{Segment: retrieval.Segment{SourceID: "library", Key: key, Title: "doc " + key, Text: "text of " + key,
					Location: retrieval.Location{Path: key + ".md", StartLine: 3}}, Keys: []string{key}, Score: 0.5 - float64(i)/10, RRF: 0.5}
				items = append(items, conversation.ContextItem{Kind: "segment", Title: seg.Title, Text: seg.Text, Score: seg.Score, Segment: &seg})
			}
			return items, nil
		},
	})
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	events := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "what?"}))
	sources := events[conversationservice.EventSources]
	if len(sources) != 1 || len(sources[0].Segments) != 2 || sources[0].Segments[0].Number != 1 || sources[0].Segments[1].SegmentKey != "k2" {
		t.Fatalf("sources %+v", sources)
	}
	system := chat.got[0].System
	if !strings.Contains(system, "## [1] doc k1\ntext of k1") || !strings.Contains(system, "## [2] doc k2") || !strings.Contains(system, "cite it") {
		t.Errorf("system prompt %q", system)
	}
	done := events[conversationservice.EventDone][0].Round
	rounds := s.Rounds(conversationservice.RoundsArgs{ServiceArgs: args, ConversationID: done.ConversationID})
	if len(rounds.Segments) != 2 || rounds.Segments[0].RoundID != done.ID || !strings.Contains(rounds.Segments[1].Location, `"path":"k2.md"`) {
		t.Errorf("stored segments %+v", rounds.Segments)
	}

	counts := func() map[string][2]int {
		var rows []retrieval.SegmentFeedback
		s.db.Find(&rows)
		out := map[string][2]int{}
		for _, r := range rows {
			out[r.SegmentKey] = [2]int{r.Up, r.Down}
		}
		return out
	}
	feedback := func(f retrieval.Feedback) conversationservice.FeedbackResponse {
		return s.Feedback(conversationservice.FeedbackArgs{ServiceArgs: args, RoundID: done.ID, Feedback: f})
	}
	if resp := feedback(retrieval.FeedbackCorrect); resp.Common.Code != http.StatusOK || resp.Round.Feedback != "up" || counts()["k1"] != [2]int{1, 0} {
		t.Errorf("👍 %+v, counts %v", resp, counts())
	}
	feedback(retrieval.FeedbackIncorrect)
	if c := counts(); c["k1"] != [2]int{0, 1} || c["k2"] != [2]int{0, 1} {
		t.Errorf("👍 → 👎 counts %v", c)
	}
	// 🚫 takes the vote back and drops the round from history
	feedback(retrieval.FeedbackIgnore)
	if c := counts(); c["k1"] != [2]int{0, 0} {
		t.Errorf("🚫 counts %v", c)
	}
	collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: done.ConversationID, Text: "next"}))
	if msgs := chat.got[len(chat.got)-1].Messages; len(msgs) != 1 {
		t.Errorf("an ignored round is history: %+v", msgs)
	}
	if code := feedback("bogus").Common.Code; code != http.StatusBadRequest {
		t.Errorf("bogus feedback: code %d", code)
	}
	other := args
	other.Principal.UserID = 99
	if code := s.Feedback(conversationservice.FeedbackArgs{ServiceArgs: other, RoundID: done.ID, Feedback: retrieval.FeedbackCorrect}).Common.Code; code != http.StatusNotFound {
		t.Errorf("another user's round: code %d", code)
	}
}

func TestPinnedSegmentsGoFirstAndFollowersFollow(t *testing.T) {
	chat := &fakeChat{}
	s, _ := newTestService(t, chat)
	segment := func(source, key string, score float64, pinned bool) conversation.ContextItem {
		seg := retrieval.Scored{Segment: retrieval.Segment{SourceID: source, Key: key, Title: key, Text: "text of " + key}, Keys: []string{key}, Score: score, Pinned: pinned}
		return conversation.ContextItem{Title: key, Text: seg.Text, Score: score, Segment: &seg}
	}
	var window []conversation.Round
	conversation.ContextProviders.Register(s.registry, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: "memory"},
		Gather: func(args conversation.GatherArgs) ([]conversation.ContextItem, error) {
			window = args.Window
			return []conversation.ContextItem{segment("chronicle", "e1", 0, true)}, nil
		},
	})
	conversation.ContextProviders.Register(s.registry, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: "search"},
		Gather: func(args conversation.GatherArgs) ([]conversation.ContextItem, error) {
			// e1 again, unpinned and better scored: it goes in once, pinned
			return []conversation.ContextItem{segment("library", "c1", 0.9, false), segment("chronicle", "e1", 0.8, false)}, nil
		},
	})
	var followed []conversation.FollowArgs
	var verdicts []retrieval.Feedback
	conversation.RoundFollowers.Register(s.registry, conversation.RoundFollower{
		Descriptor: extensions.Descriptor{ID: "memory"},
		Follow: func(args conversation.FollowArgs) (*conversation.Followed, error) {
			followed = append(followed, args)
			return &conversation.Followed{Summary: "Memory: updated *e1*"}, nil
		},
		Feedback: func(_ features.ServiceArgs, _ conversation.Round, _, to retrieval.Feedback) {
			verdicts = append(verdicts, to)
		},
	})
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	first := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Text: "hi",
		ContextSettings: map[string]json.RawMessage{"memory": json.RawMessage(`{"enabled": true, "checked": ["5"]}`)}}))
	sources := first[conversationservice.EventSources][0].Segments
	if len(sources) != 2 || sources[0].SegmentKey != "e1" || !sources[0].Pinned || sources[0].Number != 1 || sources[1].SegmentKey != "c1" {
		t.Fatalf("sources %+v", sources)
	}
	if strings.Index(chat.got[0].System, "[1] e1") > strings.Index(chat.got[0].System, "[2] c1") {
		t.Errorf("pinned segment not first: %s", chat.got[0].System)
	}
	f := first[conversationservice.EventFollowed]
	if len(f) != 1 || f[0].Follower != "memory" || f[0].Followed.Summary != "Memory: updated *e1*" {
		t.Fatalf("followed %+v", f)
	}
	done := first[conversationservice.EventDone][0].Round
	if len(followed) != 1 || followed[0].Round.ID != done.ID || len(followed[0].Segments) != 2 ||
		string(followed[0].Settings) != `{"enabled": true, "checked": ["5"]}` || followed[0].Context().Err() != nil {
		t.Errorf("follow args %+v", followed)
	}

	// The next round's providers see the window
	collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: done.ConversationID, Text: "again"}))
	if len(window) != 1 || window[0].ID != done.ID {
		t.Errorf("window %+v", window)
	}
	s.Feedback(conversationservice.FeedbackArgs{ServiceArgs: args, RoundID: done.ID, Feedback: retrieval.FeedbackIgnore})
	s.Feedback(conversationservice.FeedbackArgs{ServiceArgs: args, RoundID: done.ID, Feedback: retrieval.FeedbackIgnore})
	if len(verdicts) != 1 || verdicts[0] != retrieval.FeedbackIgnore {
		t.Errorf("verdicts %v", verdicts)
	}
}
