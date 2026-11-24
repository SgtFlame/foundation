package activity

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

// focusSummary is a focused tab in a few words: opened "Refund for the
// second charge?".
func focusSummary(t Thing) string {
	if t.Label != "" {
		return fmt.Sprintf("opened %q", cut(t.Label, 80))
	}
	return fmt.Sprintf("opened %s %s", t.Type, t.ID)
}

// uiSummary is an event on a page in a few words: clicked "Billing" in
// Mail, or the handler when the node shows no text.
func uiSummary(e Entry) string {
	verb := "used"
	switch e.Event {
	case "click", "":
		verb = "clicked"
	case "select", "change", "edit", "toggle", "check":
		verb = "changed"
	case "submit", "save":
		verb = "saved"
	case "search":
		verb = "searched in"
	}
	what := fmt.Sprintf("%q", e.Label)
	if e.Label == "" {
		what = handlerWords(e.Handler)
	}
	s := verb + " " + what
	if e.Title != "" {
		s += " in " + cut(e.Title, 60)
	}
	return s
}

// handlerWords names a handler by what it does: hello-onSelect,
// prefab:root/x#onAdd and onAdd are "select" and "add".
func handlerWords(h string) string {
	if i := strings.LastIndexAny(h, "#-"); i >= 0 && i < len(h)-1 {
		h = h[i+1:]
	}
	if rest, ok := strings.CutPrefix(h, "on"); ok && rest != "" && unicode.IsUpper([]rune(rest)[0]) {
		h = rest
	}
	return strings.ToLower(humanize(h))
}

// commandSummary is a command's event in a few words: Label thread
// "Refund for the second charge?" (Labels: Billing).
func commandSummary(e Entry) string {
	var b strings.Builder
	switch {
	case e.Command == "record.create" || e.Change == "created" && strings.HasPrefix(e.Command, "record."):
		b.WriteString("created " + e.EntityName)
	case e.Command == "record.delete":
		b.WriteString("deleted " + e.EntityName)
	case strings.HasPrefix(e.Command, "record.") || e.Command == "":
		b.WriteString("changed " + e.EntityName)
	default:
		b.WriteString(e.CommandTitle)
		if e.Change == "created" {
			b.WriteString(", making a " + e.EntityName)
		}
	}
	if e.RecordTitle != "" {
		fmt.Fprintf(&b, " %q", e.RecordTitle)
	}
	var parts []string
	for _, f := range e.Fields {
		if f.Omitted {
			parts = append(parts, f.Name)
			continue
		}
		parts = append(parts, f.Name+": "+show(f.Value))
	}
	if len(parts) > 0 {
		b.WriteString(" (" + strings.Join(parts, ", ") + ")")
	}
	return b.String()
}

// show is a field's value in a summary.
func show(v any) string {
	switch v := v.(type) {
	case nil:
		return "none"
	case string:
		return cut(v, 40)
	case []string:
		if len(v) == 0 {
			return "none"
		}
		return cut(strings.Join(v, ", "), 60)
	case []any:
		if len(v) == 0 {
			return "none"
		}
		var s []string
		for _, x := range v {
			s = append(s, show(x))
		}
		return cut(strings.Join(s, ", "), 60)
	case bool:
		if v {
			return "yes"
		}
		return "no"
	}
	return cut(fmt.Sprint(v), 40)
}

func sortFields(fs []Field) {
	slices.SortFunc(fs, func(a, b Field) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })
}

// sscanSubject reads a person's subject back: act.<tenant>.<user>.
func sscanSubject(subject string, tenant, user *uint64) (int, error) {
	return fmt.Sscanf(subject, "act.%d.%d", tenant, user)
}

// Params are activity.mine's parameters.
type Params struct {
	// After keeps the gestures that ended after this time
	After *time.Time `json:"after,omitempty"`
	// Limit keeps the newest gestures, 50 when left out
	Limit int `json:"limit,omitempty"`
	// Sendable leaves out the gestures the person dismissed: what may be
	// sent to a model
	Sendable bool `json:"sendable,omitempty"`
}

// DefaultLimit is how many gestures activity.mine gives when not told.
const DefaultLimit = 50

// mergeWithin is how close an event must follow the same event on the
// same node, causing no commands, to be merged into it.
const mergeWithin = 30 * time.Second

// Group is a person's entries, in the order stored, as gestures newest
// last: a clear drops what came before it, a dismiss marks gestures,
// entries older than the settings keep are left out, and the same event
// on the same node repeated within 30 seconds with no commands is one
// gesture.
func Group(entries []Entry, set Settings, now time.Time, q Params) Activity {
	out := Activity{Recording: set.Record, Gestures: []Gesture{}}
	if !set.Record {
		return out
	}
	oldest := now.Add(-set.Keep())
	dismissed := map[string]bool{}
	var kept []Entry
	for _, e := range entries {
		switch e.Kind {
		case KindClear:
			kept, dismissed = nil, map[string]bool{}
			continue
		case KindDismiss:
			for _, id := range e.IDs {
				dismissed[id] = true
			}
			continue
		}
		if e.Time.Before(oldest) {
			continue
		}
		kept = append(kept, e)
	}
	byID := map[string]int{}
	var gs []Gesture
	for _, e := range kept {
		id := cmp.Or(e.Request, e.ID)
		i, ok := byID[id]
		if !ok {
			i = len(gs)
			byID[id] = i
			gs = append(gs, Gesture{ID: id, Start: e.Time, Time: e.Time})
		}
		g := &gs[i]
		g.Entries = append(g.Entries, e)
		if e.Time.Before(g.Start) {
			g.Start = e.Time
		}
		if e.Time.After(g.Time) {
			g.Time = e.Time
		}
	}
	for i := range gs {
		finish(&gs[i], dismissed[gs[i].ID])
	}
	slices.SortStableFunc(gs, func(a, b Gesture) int { return a.Start.Compare(b.Start) })
	gs = merge(gs)
	for _, g := range gs {
		if q.Sendable && g.Dismissed || q.After != nil && !g.Time.After(*q.After) {
			continue
		}
		out.Gestures = append(out.Gestures, g)
	}
	limit := cmp.Or(q.Limit, DefaultLimit)
	if len(out.Gestures) > limit {
		out.Gestures = out.Gestures[len(out.Gestures)-limit:]
	}
	return out
}

// finish orders a gesture's entries (the click first, then its commands)
// and gives it its app, subject and summary.
func finish(g *Gesture, dismissed bool) {
	rank := map[string]int{KindFocus: 0, KindUI: 1, KindCommand: 2, KindOp: 3}
	slices.SortStableFunc(g.Entries, func(a, b Entry) int {
		return cmp.Or(cmp.Compare(rank[a.Kind], rank[b.Kind]), a.Time.Compare(b.Time))
	})
	g.Dismissed = dismissed
	var lead, rest []string
	for _, e := range g.Entries {
		g.App = cmp.Or(g.App, e.App)
		if g.Subject == nil && e.Subject != nil {
			g.Subject = e.Subject
		}
		switch e.Kind {
		case KindFocus, KindUI:
			lead = append(lead, e.Summary)
		default:
			rest = append(rest, e.Summary)
		}
	}
	if g.Subject == nil {
		for _, e := range g.Entries {
			if e.Kind == KindCommand && e.Target != "" {
				g.Subject = &Thing{Type: e.Entity, ID: e.Target, Label: e.RecordTitle}
				break
			}
		}
	}
	rest = compact(rest)
	switch {
	case len(lead) > 0 && len(rest) > 0:
		g.Summary = strings.Join(lead, "; ") + " → " + strings.Join(rest, "; ")
	case len(lead) > 0:
		g.Summary = strings.Join(lead, "; ")
	default:
		g.Summary = strings.Join(rest, "; ")
	}
}

// compact keeps a summary repeated by several events once, with a count.
func compact(ss []string) []string {
	var out []string
	count := map[string]int{}
	for _, s := range ss {
		if count[s] == 0 {
			out = append(out, s)
		}
		count[s]++
	}
	for i, s := range out {
		if n := count[s]; n > 1 {
			out[i] = fmt.Sprintf("%s (%d times)", s, n)
		}
	}
	return out
}

// merge folds a page event that only repeats the one before it (the same
// handler on the same node, no commands, within 30 seconds) into it.
func merge(gs []Gesture) []Gesture {
	var out []Gesture
	for _, g := range gs {
		if n := len(out); n > 0 && repeats(out[n-1], g) {
			last := &out[n-1]
			last.Repeats++
			last.Time = g.Time
			last.Dismissed = last.Dismissed && g.Dismissed
			last.Entries = append(last.Entries, g.Entries...)
			continue
		}
		out = append(out, g)
	}
	return out
}

func repeats(a, b Gesture) bool {
	only := func(g Gesture) *Entry {
		if len(g.Entries) == 0 {
			return nil
		}
		for _, e := range g.Entries {
			if e.Kind != KindUI {
				return nil
			}
		}
		return &g.Entries[len(g.Entries)-1]
	}
	x, y := only(a), only(b)
	return x != nil && y != nil && x.Page == y.Page && x.Handler == y.Handler && slices.Equal(x.Path, y.Path) && b.Start.Sub(a.Time) <= mergeWithin
}
