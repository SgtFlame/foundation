package infra

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/text"
)

// DiffKeys are the configuration document's keyed lists, for text.DataDiff.
var DiffKeys = map[string]string{
	"accounts": "id", "sites": "id", "clusters": "id", "hostPools": "id", "profiles": "id",
	"stacks": "id", "placements": "id", "overrides": "id",
	"profiles[].units": "name", "profiles[].files": "path", "placements[].sites": "site",
}

// Change is one change between two configurations, readable: Summary says
// it as a change to a thing (host pool general on west: count 3 → 5).
type Change struct {
	text.DataChange
	Kind    string `json:"kind,omitempty"` // the list it is in
	Item    string `json:"item,omitempty"` // the item's id
	Summary string `json:"summary"`
}

// Diff is the changes from a to b, in the order DataDiff gives.
func Diff(a, b Document) []Change {
	raw := text.DataDiff(a.Normalized(), b.Normalized(), DiffKeys)
	out := make([]Change, 0, len(raw))
	for _, c := range raw {
		if c.Op == "moved" {
			continue // lists are kept sorted by id: a move means nothing
		}
		ch := Change{DataChange: c}
		ch.Kind, ch.Item = splitPath(c.Path)
		ch.Summary = summarize(ch, a, b)
		out = append(out, ch)
	}
	return out
}

// splitPath is a path's list and item id: hostPools[general].count gives
// hostPools and general.
func splitPath(p string) (string, string) {
	open := strings.Index(p, "[")
	if open < 0 {
		return strings.SplitN(p, ".", 2)[0], ""
	}
	end := strings.Index(p[open:], "]")
	if end < 0 {
		return p[:open], ""
	}
	return p[:open], p[open+1 : open+end]
}

var kindNames = map[string]string{
	"accounts": "provider account", "sites": "site", "clusters": "cluster", "hostPools": "host pool",
	"profiles": "instance profile", "stacks": "stack", "placements": "placement", "overrides": "tenant override",
	"settings": "settings",
}

func summarize(c Change, a, b Document) string {
	name := kindNames[c.Kind]
	if name == "" {
		name = c.Kind
	}
	thing := name
	if c.Item != "" {
		thing = name + " " + c.Item
		if c.Kind == "hostPools" || c.Kind == "clusters" {
			if site := siteOf(c.Kind, c.Item, b, a); site != "" {
				thing += " on " + site
			}
		}
	}
	field := ""
	if i := strings.Index(c.Path, "]."); i >= 0 {
		field = c.Path[i+2:]
	} else if c.Item == "" {
		field = strings.TrimPrefix(strings.TrimPrefix(c.Path, c.Kind), ".")
	}
	switch {
	case field == "" && c.Op == "added":
		return "adds " + thing
	case field == "" && c.Op == "removed":
		return "removes " + thing
	case c.Op == "added":
		return fmt.Sprintf("%s: %s set to %s", thing, field, short(c.After))
	case c.Op == "removed":
		return fmt.Sprintf("%s: %s cleared (was %s)", thing, field, short(c.Before))
	default:
		return fmt.Sprintf("%s: %s %s → %s", thing, field, short(c.Before), short(c.After))
	}
}

func siteOf(kind, id string, docs ...Document) string {
	for _, d := range docs {
		switch kind {
		case "hostPools":
			for _, h := range d.HostPools {
				if h.ID == id {
					return h.Site
				}
			}
		case "clusters":
			for _, c := range d.Clusters {
				if c.ID == id {
					return c.Site
				}
			}
		}
	}
	return ""
}

func short(v any) string {
	raw, _ := json.Marshal(v)
	s := string(raw)
	if str, ok := v.(string); ok {
		s = str
	}
	if len(s) > 60 {
		s = s[:57] + "…"
	}
	return s
}

func contributeDiff(reg *extensions.Registry) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "infra.diff", Source: source}, Title: "Compare configurations",
		Description: "The changes between two configuration versions, as changes to things: [{op: added | removed | changed, path, kind, item, before, after, summary}], where summary reads like 'host pool general on west: count 3 → 5'. from and to are version numbers; 0 (or left out for to) is the draft, and from left out is the draft's last committed version, so {} is what the draft changes. It follows the draft as it changes.",
		Params:      json.RawMessage(`{"type":"object","properties":{"config":{"type":"string","description":"The configuration, main when left out"},"from":{"type":"integer","minimum":0,"description":"The old version; the head when left out, 0 for an empty configuration when there is none"},"to":{"type":"integer","minimum":0,"description":"The new version; 0 or left out for the draft"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{"from":1,"to":2}`)},
		Action:      ActionRead,
		Run:         runDiff,
	})
}

func runDiff(f live.Feed) error {
	var p struct {
		Config string `json:"config"`
		From   *int   `json:"from"`
		To     int    `json:"to"`
	}
	_ = json.Unmarshal(f.Params(), &p)
	if p.Config == "" || !live.ValidToken(p.Config) {
		p.Config = DefaultConfig
	}
	tenant := f.Args().Principal.TenantID
	version := func(n int) (Document, error) {
		if n == 0 {
			return Document{}.Normalized(), nil
		}
		v, ok, err := f.Store().Get(f.Context(), tenant, VersionProjector, VersionType+"."+VersionID(p.Config, n))
		if err != nil {
			return Document{}, err
		}
		if !ok {
			return Document{}, fmt.Errorf("there is no version %d", n)
		}
		var ver Version
		if err := json.Unmarshal(v.Value, &ver); err != nil {
			return Document{}, err
		}
		return ver.Document, nil
	}
	entries, err := f.Store().Watch(f.Context(), tenant, ConfigProjector, ConfigType+"."+p.Config)
	if err != nil {
		return err
	}
	cfg := Config{Draft: Document{}.Normalized()}
	pos := live.Position{}
	started := false
	send := func() error {
		from := cfg.Head
		if p.From != nil {
			from = *p.From
		}
		a, err := version(from)
		if err != nil {
			return err
		}
		b := cfg.Draft
		if p.To > 0 {
			if b, err = version(p.To); err != nil {
				return err
			}
		}
		changes := Diff(a, b)
		if !started {
			f.Snapshot(changes, pos)
			started = true
		} else {
			f.Change(live.Delta{Op: "replace", Value: mustJSON(changes)}, pos)
		}
		return nil
	}
	for e := range entries {
		if e == nil {
			if err := send(); err != nil {
				return err
			}
			continue
		}
		pos = pos.Merge(e.Value.Position())
		if !e.Deleted {
			_ = json.Unmarshal(e.Value.Value, &cfg)
		}
		if started {
			if err := send(); err != nil {
				return err
			}
		}
	}
	return nil
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
