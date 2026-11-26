package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Dialogues (forge 375 S3, a thin 381): what a person says when the
// player talks to them, as built-in JSON for now; definitions edited by
// ops come with the dialogue editor. A dialogue is nodes of the speaker's
// text, each with the player's choices, which lead to another node or end
// the talk.

//go:embed dialogues/*.json
var dialogueFiles embed.FS

// Dialogue is one conversation.
type Dialogue struct {
	ID          string                  `json:"id"`
	Title       string                  `json:"title"`
	Description string                  `json:"description"`
	Speaker     string                  `json:"speaker"`
	Start       string                  `json:"start"`
	Nodes       map[string]DialogueNode `json:"nodes"`
}

// DialogueNode is one thing the speaker says and the player's answers.
type DialogueNode struct {
	Text    string           `json:"text"`
	Choices []DialogueChoice `json:"choices"`
}

// DialogueChoice is one answer: it leads to Next, or ends the talk.
type DialogueChoice struct {
	Text string `json:"text"`
	Next string `json:"next,omitempty"`
	End  bool   `json:"end,omitempty"`
}

var dialogues = loadDialogues()

func loadDialogues() map[string]*Dialogue {
	out := map[string]*Dialogue{}
	entries, err := dialogueFiles.ReadDir("dialogues")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		raw, err := dialogueFiles.ReadFile("dialogues/" + e.Name())
		if err != nil {
			panic(err)
		}
		var d Dialogue
		if err := json.Unmarshal(raw, &d); err != nil {
			panic(fmt.Sprintf("dialogue %s: %v", e.Name(), err))
		}
		if msg := d.Check(); msg != "" {
			panic(fmt.Sprintf("dialogue %s: %s", e.Name(), msg))
		}
		out[d.ID] = &d
	}
	return out
}

// Check reports what is wrong with a dialogue: a start or next naming no
// node, or a node with no way on.
func (d *Dialogue) Check() string {
	if _, ok := d.Nodes[d.Start]; !ok {
		return fmt.Sprintf("its start %q is not a node", d.Start)
	}
	ids := make([]string, 0, len(d.Nodes))
	for id := range d.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := d.Nodes[id]
		if len(n.Choices) == 0 {
			return fmt.Sprintf("node %q has no choices", id)
		}
		for _, c := range n.Choices {
			if c.End {
				continue
			}
			if _, ok := d.Nodes[c.Next]; !ok {
				return fmt.Sprintf("node %q leads to %q, which is not a node", id, c.Next)
			}
		}
	}
	return ""
}

// DialogueOf is the built-in dialogue of that id.
func DialogueOf(id string) (*Dialogue, bool) {
	d, ok := dialogues[id]
	return d, ok
}

func contributeDialogues(reg *extensions.Registry) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.dialogues", Source: source}, Title: "Dialogues",
		Description: "What people say when the player talks to them, by id: {id, title, description, speaker, start, nodes: {<id>: {text, choices: [{text, next?, end?}]}}}. Built in for now; they do not change while the server runs.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "game.read",
		Run: func(f live.Feed) error {
			f.Snapshot(dialogues, live.Position{})
			<-f.Context().Done()
			return nil
		},
	})
}
