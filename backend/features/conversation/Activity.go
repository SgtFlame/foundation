package conversation

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
)

// ActivityID is the activity context provider's id (forge 15d): the
// person's recent gestures, sent with a message while it is on.
const ActivityID = "activity"

// DefaultLookBack is how far back a conversation's first message looks
// for the person's gestures, unless the activity settings say otherwise
// (forge 15d question 13). Later messages look back to the round before.
const DefaultLookBack = 30 * time.Minute

// Gesture is one of the person's gestures sent with a round: what
// activity.mine gives, without its entries. Its summary quotes every
// label.
type Gesture struct {
	ID      string    `json:"id"`
	Start   time.Time `json:"start"`
	Time    time.Time `json:"time"`
	App     string    `json:"app,omitempty"`
	Summary string    `json:"summary"`
	Repeats int       `json:"repeats,omitempty"`
	// Asks are the values the person marked to ask for each run when this
	// is automated (forge 15g)
	Asks []GestureAsk `json:"asks,omitempty"`
}

// GestureAsk is a value to ask for each run: the field's name, the
// question and the default answer (none for an omitted value).
type GestureAsk struct {
	Name     string `json:"name"`
	Question string `json:"question"`
	Default  any    `json:"default,omitempty"`
}

// ActivityReader reads the caller's own gestures that ended after a time,
// leaving out the dismissed ones, newest last, and says whether their
// activity is recorded at all. Send reads it once per round
// (conversationimpl.WithActivity, wired by serve.go to the activity
// feature), so the prompt, the activity pin and the stored round agree.
type ActivityReader func(args features.ServiceArgs, after time.Time) (gestures []Gesture, recording bool, err error)

// ActivitySettings are the activity provider's settings.
type ActivitySettings struct {
	Enabled         *bool `json:"enabled"`
	LookBackMinutes int   `json:"lookBackMinutes"`
}

// ActivitySettingsOf decodes the provider's settings.
func ActivitySettingsOf(raw json.RawMessage) ActivitySettings {
	var s ActivitySettings
	_ = json.Unmarshal(raw, &s)
	return s
}

// On reports whether the activity is sent: on unless set to false.
func (s ActivitySettings) On() bool { return s.Enabled == nil || *s.Enabled }

// LookBack is how far back a first message looks.
func (s ActivitySettings) LookBack() time.Duration {
	if s.LookBackMinutes <= 0 {
		return DefaultLookBack
	}
	return time.Duration(s.LookBackMinutes) * time.Minute
}

// Gestures are the gestures a round was sent with, from its Activity.
func (r Round) Gestures() []Gesture {
	if r.Activity == "" {
		return nil
	}
	var gs []Gesture
	_ = json.Unmarshal([]byte(r.Activity), &gs)
	return gs
}
