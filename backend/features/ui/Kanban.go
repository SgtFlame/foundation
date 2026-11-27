package ui

import (
	"fmt"
	"maps"
	"slices"
)

// KanbanType is the board primitive (forge 30b): columns of cards whose
// one child is the card template, evaluated per card as a repeat's row is.
const KanbanType = "mui.kanban"

// kanban evaluates a board as a region of its own: its cards are read (a
// records page is taken as its records) and sent as placed {id, column,
// version, key}, with the card template once per card, keyed <board>_<id>
// (the key placed names). The cards themselves are not sent: the browser places
// the rendered children by placed.
func (e *evaluator) kanban(r *Render, sc *scope, n *Node, path []string, key string) *Node {
	return r.Region(key, func(r *Render) *Node {
		shell := *n
		shell.Children = nil
		out := e.primitive(r, sc, &shell, path, key)
		if out.Type != n.Type {
			return out // its problems
		}
		v := e.boundValue(r, out, "cards")
		if m, ok := v.(map[string]any); ok {
			v = m["records"]
		}
		cards, ok := v.([]any)
		if v != nil && !ok {
			return ErrorNode(key, []Problem{{Message: "cards is not a list"}})
		}
		delete(out.Props, "cards")
		delete(out.Bind, "cards")
		field, _ := out.Props["columnField"].(string)
		if field == "" {
			field = "status"
		}
		as, _ := n.Props["as"].(string)
		if as == "" {
			as = "card"
		}
		here := append(slices.Clone(path), key)
		placed := make([]any, 0, len(cards))
		seen := map[string]bool{}
		for i, c := range cards {
			rec, _ := c.(map[string]any)
			id := cardID(rec)
			if id == "" {
				id = fmt.Sprint(i)
			}
			if seen[id] {
				continue // a card shows once
			}
			seen[id] = true
			card := map[string]any{"id": id, "column": columnOf(rec[field]), "version": rec["_seq"]}
			placed = append(placed, card)
			if len(n.Children) == 0 {
				continue
			}
			rs := &scope{inst: sc.inst, rows: maps.Clone(sc.rows), slots: sc.slots, outer: sc.outer}
			if rs.rows == nil {
				rs.rows = map[string]any{}
			}
			rs.rows[as] = c
			k := key + "_" + unsafeKey.ReplaceAllString(id, "_")
			card["key"] = k
			out.Children = append(out.Children, e.nodes(r, rs, n.Children[0], here, k)...)
		}
		out.Props["placed"] = placed
		return out
	})
}

// cardID is a card's record id (_id, else id).
func cardID(rec map[string]any) string {
	for _, k := range []string{"_id", "id"} {
		if v, ok := rec[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}

// columnOf is the column a card's field names: a relation's id, an enum's
// option or a text value.
func columnOf(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		return cardID(v)
	case []any:
		if len(v) > 0 {
			return columnOf(v[0])
		}
		return ""
	}
	return fmt.Sprint(v)
}
