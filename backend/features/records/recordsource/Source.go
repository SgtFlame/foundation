// Package recordsource makes one searchable entity type a retrieval source
// of its own (forge 18f: Forge's Blackboards, the sections of requirements
// boards). Its segments are the records the searching principal may read,
// found through the records' search index (data.SearchRecords, which
// applies the type's access and row rules), narrowed by a partition field
// (a branch) and a collection field (a space). It may also give a tool
// that searches it alone, as kb.search does the Volumes.
package recordsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Spec is one entity type as a source.
type Spec struct {
	ID, Title, Description string
	Order                  int
	Axis                   retrieval.Axis
	// Optional and InChat as retrieval.Source has them.
	Optional, InChat bool
	// Entity is the type searched; its text fields must be searchable.
	Entity string
	// Text is the field a segment's text is read from.
	Text string
	// Partition is the field that partitions the records (a branch); a
	// search names the partitions it reads, else DefaultPartition. Empty:
	// no partitions.
	Partition, DefaultPartition string
	// Collection is the field naming the collection a record is in (a
	// space); a search may keep to some. Empty: no collections.
	Collection string
	// Lexical is the lexical ↔ semantic dial its searches run at, as
	// records.search's: above 0, records near in meaning join keyword
	// matches.
	Lexical int
	// Locate gives a found record's citation title and location.
	Locate func(r data.Record) (title string, at retrieval.Location)
}

// Contribute registers the source.
func Contribute(reg *extensions.Registry, deps func() data.Deps, s Spec) {
	retrieval.Sources.Register(reg, retrieval.Source{
		Descriptor: extensions.Descriptor{ID: s.ID, Order: s.Order, Source: "records"},
		Title:      s.Title, Axis: s.Axis, Optional: s.Optional, InChat: s.InChat,
		KeywordSearch: func(ctx context.Context, query string, k int, f retrieval.Filter) ([]retrieval.Segment, error) {
			p, meta, ok := features.CallFrom(ctx)
			if !ok {
				return nil, nil
			}
			return s.search(features.ServiceArgs{Ctx: ctx, Principal: p, Meta: meta}, deps(), query, k, f)
		},
	})
}

// search finds the records for a query within a filter's partitions and
// collections, as segments.
func (s Spec) search(args features.ServiceArgs, d data.Deps, query string, k int, f retrieval.Filter) ([]retrieval.Segment, error) {
	if d.Search == nil || slices.Contains(f.Collections, "none") {
		return nil, nil
	}
	var and []data.Filter
	if s.Partition != "" {
		parts := f.Partitions
		if len(parts) == 0 && s.DefaultPartition != "" {
			parts = []string{s.DefaultPartition}
		}
		if len(parts) > 0 {
			and = append(and, in(s.Partition, parts))
		}
	}
	if s.Collection != "" && len(f.Collections) > 0 {
		and = append(and, in(s.Collection, f.Collections))
	}
	q := data.SearchQuery{Entity: s.Entity, Text: query, Limit: min(max(k, 1), 100), LexicalSemantic: s.Lexical}
	if len(and) > 0 {
		q.Filter = &data.Filter{And: and}
	}
	res, err := data.SearchRecords(args, d, q)
	if err != nil {
		return nil, err
	}
	out := make([]retrieval.Segment, 0, len(res.Records))
	for _, r := range res.Records {
		var id, text string
		_ = json.Unmarshal(r[data.MetaID], &id)
		_ = json.Unmarshal(r[s.Text], &text)
		if strings.TrimSpace(text) == "" {
			continue
		}
		title, at := s.Entity+" "+id, retrieval.Location{Entity: s.Entity, RecordID: id}
		if s.Locate != nil {
			title, at = s.Locate(r)
		}
		sum := sha256.Sum256([]byte(text))
		// Feedback counts against the text: a section edited is judged anew
		out = append(out, retrieval.Segment{SourceID: s.ID, ID: id, Key: id + "#" + hex.EncodeToString(sum[:6]), Title: title, Text: text, Location: at})
	}
	return out, nil
}

func in(field string, values []string) data.Filter {
	raw, _ := json.Marshal(values)
	return data.Filter{Field: field, Op: "in", Value: raw}
}

// Tool is a tool searching the source alone.
type Tool struct {
	ID, Title, Description string
	Order                  int
	// PartitionArg and CollectionArg name the tool's arguments for the
	// partition and the collection (branch, space); empty leaves them out.
	PartitionArg, PartitionHelp   string
	CollectionArg, CollectionHelp string
}

// ContributeTool registers a tool searching the source s alone through
// retrieval, so its passages are numbered and cited as kb.search's are.
// The model may name partitions and collections, which only narrow: a
// run's options (an agent's spaces) keep it within theirs, and the
// principal's grants decide what it reads.
func ContributeTool(reg *extensions.Registry, retriever func() retrievalservice.Service, s Spec, t Tool) {
	props := map[string]any{
		"query": map[string]any{"type": "string", "minLength": 1, "description": "What to look for, in words likely to appear in the text"},
		"count": map[string]any{"type": "integer", "minimum": 1, "maximum": 20, "description": "How many passages, 6 by default"},
	}
	if t.PartitionArg != "" {
		props[t.PartitionArg] = map[string]any{"type": "string", "description": t.PartitionHelp}
	}
	if t.CollectionArg != "" {
		props[t.CollectionArg] = map[string]any{"type": "string", "description": t.CollectionHelp}
	}
	input, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": []string{"query"}})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: t.ID, Order: t.Order, Source: "records"},
		Title:      t.Title, Description: t.Description, Input: input, Effect: tools.EffectRead,
		Run: func(call tools.Call) (tools.Result, error) {
			var a map[string]any
			if err := json.Unmarshal(call.Args, &a); err != nil {
				return tools.Result{}, errors.New("the arguments are not an object")
			}
			query, _ := a["query"].(string)
			count, _ := a["count"].(float64)
			req := retrievalservice.RetrieveArgs{ServiceArgs: call.ServiceArgs, Query: query, TopK: min(max(int(count), 0), 20), Sources: []string{s.ID}, LexicalSemantic: s.Lexical}
			if req.TopK == 0 {
				req.TopK = 6
			}
			if v, _ := a[t.PartitionArg].(string); t.PartitionArg != "" && strings.TrimSpace(v) != "" {
				req.Partitions = []string{strings.TrimSpace(v)}
			}
			var want []string
			if v, _ := a[t.CollectionArg].(string); t.CollectionArg != "" && strings.TrimSpace(v) != "" {
				want = []string{strings.TrimSpace(v)}
			}
			req.Collections = want
			if o, ok := retrieval.OptionsFrom(call.Context()); ok {
				req.Collections = retrieval.Within(o.Collections, want)
				if len(o.Partitions) > 0 && len(req.Partitions) == 0 {
					req.Partitions = o.Partitions
				}
			}
			svc := retriever()
			if svc == nil {
				return tools.Result{}, errors.New("retrieval is not configured")
			}
			resp := svc.Retrieve(req)
			if resp.Common.Code >= 300 {
				return tools.Result{}, errors.New(resp.Common.Message)
			}
			n := len(resp.Segments)
			noun := "passages"
			if n == 1 {
				noun = "passage"
			}
			return tools.Result{Content: map[string]any{"query": query, "found": n}, Segments: resp.Segments,
				Summary: fmt.Sprintf("Found %d %s in %s for *%s*", n, noun, s.Title, strings.NewReplacer("*", "", "_", "").Replace(query))}, nil
		},
	})
}
