package impl

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	activitycontext "github.com/SgtFlame/foundation/backend/features/activity/context"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
)

// Forge 15d: a round reads the person's gestures once, since the
// conversation's previous round (the activity settings' look-back for a
// first message), and the prompt, the activity pin and the stored round
// carry the same list; with the conversation's activity off nothing is
// read.
func TestSendSendsWhatThePersonDid(t *testing.T) {
	chat := &fakeChat{}
	s, gathered := newTestService(t, chat)
	activitycontext.Contribute(s.registry)
	t0 := time.Date(2026, 11, 24, 13, 42, 0, 0, time.UTC)
	gestures := []conversation.Gesture{
		{ID: "r1", Start: t0, Time: t0, App: "mail", Summary: `opened "Refund for the second charge?"`},
		{ID: "r2", Start: t0.Add(time.Minute), Time: t0.Add(time.Minute), App: "mail", Summary: `changed "Labels" in "Refund for the second charge?" (Labels: Billing)`, Repeats: 1},
	}
	var afters []time.Time
	s.activity = func(args features.ServiceArgs, after time.Time) ([]conversation.Gesture, bool, error) {
		afters = append(afters, after)
		return gestures, true, nil
	}
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}

	before := time.Now()
	first := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, Mode: "acts", Text: "automate what I just did"}))
	done := first[conversationservice.EventDone][0].Round
	if len(afters) != 1 || afters[0].After(before.Add(-29*time.Minute)) || afters[0].Before(before.Add(-31*time.Minute)) {
		t.Fatalf("a first message should look back 30 minutes: %v (sent %v)", afters, before)
	}
	system := chat.got[0].System
	for _, want := range []string{
		"What the person did since their last message",
		`- 13:42:00 opened "Refund for the second charge?"`,
		`- 13:43:00 changed "Labels" in "Refund for the second charge?" (Labels: Billing) (and 1 more times)`,
		`* pin: opened "Refund for the second charge?"`,
		`* pin: changed "Labels"`,
	} {
		if !strings.Contains(system, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, system)
		}
	}
	if g := (*gathered)[0].Activity; len(g) != 2 || g[1].ID != "r2" {
		t.Errorf("the providers were given %+v", g)
	}
	if started := first[conversationservice.EventStarted][0].Round; len(started.Gestures()) != 2 {
		t.Errorf("the started round holds %q", started.Activity)
	}

	// Stored with the round, so a reload shows them after the stream drops them
	gestures = nil
	rounds := s.Rounds(conversationservice.RoundsArgs{ServiceArgs: args, ConversationID: done.ConversationID}).Rounds
	if got := rounds[0].Gestures(); len(got) != 2 || got[0].Summary != `opened "Refund for the second charge?"` || got[1].Repeats != 1 {
		t.Fatalf("the stored round's activity: %q", rounds[0].Activity)
	}

	// The next message looks back to the round before, and sends nothing new
	second := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: done.ConversationID, Text: "and then?"}))
	if len(afters) != 2 || !afters[1].Equal(done.CreatedAt) && afters[1].Sub(done.CreatedAt).Abs() > time.Second {
		t.Errorf("the second message looked back to %v, the first round started %v", afters[1], done.CreatedAt)
	}
	if r := second[conversationservice.EventDone][0].Round; r.Activity != "" || strings.Contains(chat.got[len(chat.got)-1].System, "What the person did") {
		t.Errorf("nothing was done, yet the round holds %q", r.Activity)
	}

	// Off for the conversation: nothing is read
	gestures = []conversation.Gesture{{ID: "r3", Start: t0, Time: t0, Summary: "clicked \"Send\""}}
	off := map[string]json.RawMessage{conversation.ActivityID: json.RawMessage(`{"enabled": false}`)}
	third := collect(s.Send(conversationservice.SendArgs{ServiceArgs: args, ConversationID: done.ConversationID, Text: "again", ContextSettings: off}))
	if len(afters) != 2 || third[conversationservice.EventDone][0].Round.Activity != "" {
		t.Errorf("with activity off it was read %d times", len(afters))
	}
}
