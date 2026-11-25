package conversationdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/conversation"
)

// Gesture is one of the person's gestures a round was sent with (forge 15d).
type Gesture struct {
	ID      string       `json:"id"`
	Start   time.Time    `json:"start"`
	Time    time.Time    `json:"time"`
	App     string       `json:"app,omitempty"`
	Summary string       `json:"summary"`
	Repeats int          `json:"repeats,omitempty"`
	Asks    []GestureAsk `json:"asks,omitempty"` // forge 15g
}

// GestureAsk is a value to ask for each run.
type GestureAsk struct {
	Name     string `json:"name"`
	Question string `json:"question"`
	Default  any    `json:"default,omitempty"`
}

func GestureFrom(g conversation.Gesture) Gesture {
	out := Gesture{ID: g.ID, Start: g.Start, Time: g.Time, App: g.App, Summary: g.Summary, Repeats: g.Repeats}
	for _, a := range g.Asks {
		out.Asks = append(out.Asks, GestureAsk{Name: a.Name, Question: a.Question, Default: a.Default})
	}
	return out
}
