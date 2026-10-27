// Package chronicletools contributes Chronicle's tools
// (documentation/plans/Tools.md): chronicle.search and chronicle.getEntry,
// which cite the entries they return, and chronicle.updateEntry, which asks
// first and keeps the summaries it replaces, to restore.
package chronicletools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	chroniclesteps "github.com/SgtFlame/foundation/backend/features/chronicle/steps"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Deps are what the tools call, created after the registry.
type Deps struct {
	Retrieval retrievalservice.Service
	Entries   chronicleservice.Service
}

const source = "chronicle"

// Contribute registers the Chronicle tools.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "chronicle.search", Order: 200, Source: source},
		Title:       "Search Chronicles",
		Description: "Searches the user's Chronicles, their memory of people, projects, events and notes, by meaning and keywords. Returns the best entries, each with its id and a [n] to cite it by.",
		Input:       json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"description":"What to look for"},"count":{"type":"integer","minimum":1,"maximum":20,"description":"How many entries, 5 by default"}},"required":["query"]}`),
		Effect:      tools.EffectRead,
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct {
				Query string
				Count int
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			if args.Count == 0 {
				args.Count = 5
			}
			resp := deps().Retrieval.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: call.ServiceArgs, Query: args.Query, TopK: args.Count, Sources: []string{chroniclesteps.SourceID}})
			if resp.Common.Code >= 300 {
				return tools.Result{}, errors.New(resp.Common.Message)
			}
			return tools.Result{
				Content:  map[string]any{"query": args.Query, "found": len(resp.Segments)},
				Segments: resp.Segments,
				Summary:  fmt.Sprintf("Found %d %s for *%s*", len(resp.Segments), map[bool]string{true: "Chronicle", false: "Chronicles"}[len(resp.Segments) == 1], strings.ReplaceAll(args.Query, "*", "")),
			}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "chronicle.getEntry", Order: 210, Source: source},
		Title:       "Read a Chronicle",
		Description: "Reads one of the user's Chronicle entries in full: its type, title, summaries, full context, fields, parent and related entries.",
		Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","minLength":1,"description":"The entry's id, from chronicle.search or the context"}},"required":["id"]}`),
		Effect:      tools.EffectRead,
		Run: func(call tools.Call) (tools.Result, error) {
			id, err := entryID(call)
			if err != nil {
				return tools.Result{}, err
			}
			resp := deps().Entries.Get(chronicleservice.GetArgs{ServiceArgs: call.ServiceArgs, ID: id})
			if resp.Common.Code >= 300 {
				return tools.Result{}, errors.New(resp.Common.Message)
			}
			e := resp.Entry
			related := []string{}
			for _, l := range resp.Links {
				other := l.ToID
				if other == e.ID {
					other = l.FromID
				}
				related = append(related, strconv.FormatUint(other, 10))
			}
			content := map[string]any{
				"id": strconv.FormatUint(e.ID, 10), "type": e.Type, "status": e.Status, "fields": json.RawMessage(orEmpty(e.Fields)),
				"fullContext": e.FullContext, "related": related,
			}
			if e.ParentID != 0 {
				content["parentId"] = strconv.FormatUint(e.ParentID, 10)
			}
			seg := retrieval.Scored{Segment: retrieval.Segment{
				SourceID: chroniclesteps.SourceID, ID: strconv.FormatUint(e.ID, 10), Key: strconv.FormatUint(e.ID, 10),
				Title: e.Title, Text: e.Text(), Location: retrieval.Location{EntryID: e.ID},
			}, Reason: "chronicle.getEntry"}
			return tools.Result{Content: content, Segments: []retrieval.Scored{seg}, Summary: fmt.Sprintf("Read *%s*", strings.ReplaceAll(e.Title, "*", ""))}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "chronicle.updateEntry", Order: 220, Source: source},
		Title:       "Update a Chronicle",
		Description: "Changes one of the user's Chronicle entries: its title, short summary, full summary or full context. Fields left out keep their value. Read the entry first, and keep what is still true. The user approves it first, and can restore the earlier summaries.",
		Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","minLength":1},"title":{"type":"string","minLength":1},"shortSummary":{"type":"string","description":"One or two sentences"},"fullSummary":{"type":"string","description":"The core facts, in markdown"},"fullContext":{"type":"string","description":"The detailed, raw record"}},"required":["id"]}`),
		Effect:      tools.EffectWrite,
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct {
				ID                                            string
				Title, ShortSummary, FullSummary, FullContext *string
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			id, err := entryID(call)
			if err != nil {
				return tools.Result{}, err
			}
			if args.Title == nil && args.ShortSummary == nil && args.FullSummary == nil && args.FullContext == nil {
				return tools.Result{}, errors.New("nothing to change: give at least one of title, shortSummary, fullSummary or fullContext")
			}
			d := deps()
			got := d.Entries.Get(chronicleservice.GetArgs{ServiceArgs: call.ServiceArgs, ID: id})
			if got.Common.Code >= 300 {
				return tools.Result{}, errors.New(got.Common.Message)
			}
			e := got.Entry
			in := chronicleservice.EntryInput{Type: e.Type, Title: e.Title, ShortSummary: e.ShortSummary, FullSummary: e.FullSummary,
				FullContext: e.FullContext, Fields: json.RawMessage(orEmpty(e.Fields)), ParentID: e.ParentID}
			var changed []string
			set := func(field *string, to *string, name string) {
				if to != nil && *to != *field {
					*field = *to
					changed = append(changed, name)
				}
			}
			set(&in.Title, args.Title, "title")
			set(&in.ShortSummary, args.ShortSummary, "short summary")
			set(&in.FullSummary, args.FullSummary, "full summary")
			set(&in.FullContext, args.FullContext, "full context")
			if len(changed) == 0 {
				return tools.Result{Content: map[string]any{"updated": false, "note": "The entry already says this."}, Summary: fmt.Sprintf("*%s* already said this", e.Title)}, nil
			}
			resp := d.Entries.Update(chronicleservice.UpdateArgs{ServiceArgs: call.ServiceArgs, ID: id, EntryInput: in,
				RoundID: call.Meta.RoundID, ConversationID: call.Meta.ConversationID})
			if resp.Common.Code >= 300 {
				return tools.Result{}, errors.New(resp.Common.Message)
			}
			return tools.Result{
				Content: map[string]any{"updated": true, "changed": changed},
				Summary: fmt.Sprintf("Updated the %s of *%s*", strings.Join(changed, ", "), strings.ReplaceAll(resp.Entry.Title, "*", "")),
			}, nil
		},
	})
}

// entryID is a call's "id" argument as an entry ID.
func entryID(call tools.Call) (uint64, error) {
	args, err := tools.Decode[struct{ ID string }](call)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseUint(strings.TrimSpace(args.ID), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%q is not an entry id", args.ID)
	}
	return id, nil
}

func orEmpty(fields string) string {
	if strings.TrimSpace(fields) == "" {
		return "{}"
	}
	return fields
}
