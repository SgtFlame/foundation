package manifests

import (
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// undo sets an app.* command's inverse (forge 06a): the ops that take the
// draft back to the state the command was decided against, read from that
// state. A list item comes back at its index, and a changed item is set
// back by a merge patch of only what changed, so undoing it leaves the
// fields others changed since.
func (c *decision) undo(command string, events []live.NewEvent) {
	if c.Op == nil {
		return
	}
	id := c.Target.ID
	m := c.manifest()
	op := func(command string, args any) live.Op { return live.OpOf(command, id, args) }
	switch command {
	case "app.publish":
		c.NoUndo(id, "a published version stays; publish again to replace it")
		return
	case "app.create":
		c.NoUndo(id, "a new app cannot be deleted yet")
		return
	}
	var ops []live.Op
	for _, e := range events {
		if e.Target != nil {
			continue
		}
		var inv live.Op
		switch d := e.Data.(type) {
		case Info:
			var in Info
			if d.Name != nil {
				in.Name = &m.Name
			}
			if d.Icon != nil {
				in.Icon = &m.Icon
			}
			if d.Description != nil {
				in.Description = &m.Description
			}
			if d.Home != nil {
				in.Home = &m.Home
			}
			if d.Frame != nil {
				in.Frame = &m.Frame
			}
			if d.Requires != nil {
				in.Requires = &m.Requires
			}
			inv = op("app.setInfo", in)
		case Added:
			l, _ := listOfEvent(e.Type)
			inv = op("app.remove"+title(l.Item), map[string]any{keyArg(l): l.keyOf(d.Item)})
		case Updated:
			l, _ := listOfEvent(e.Type)
			inv = op("app.update"+title(l.Item), c.updateInverse(l, d))
		case map[string]any:
			switch e.Type {
			case EventNavSet:
				inv = op("app.setNav", map[string]any{"nav": m.Nav})
			case EventSettingsSet:
				// An empty schema is none; schema is required
				settings := map[string]any{"schema": m.Settings.Schema}
				if m.Settings.Schema == nil {
					settings["schema"] = map[string]any{}
				}
				if m.Settings.Defaults != nil {
					settings["defaults"] = m.Settings.Defaults
				}
				inv = op("app.setSettingsSchema", settings)
			case EventManifestSet:
				inv = op("app.setManifest", map[string]any{"manifest": *m})
			case EventSeedSet:
				seed := c.draft.Seed
				if seed == nil {
					seed = apps.Seed{}
				}
				inv = op("app.setSeed", map[string]any{"seed": seed})
			default:
				if l, ok := listOfEvent(e.Type); ok && e.Type == l.Removed() {
					inv = op("app.add"+title(l.Item), c.addBack(l, d["key"].(string)))
				}
			}
		}
		if inv.Command == "" {
			c.NoUndo(id, "its inverse cannot be read")
			return
		}
		ops = append([]live.Op{inv}, ops...)
	}
	c.Undo(id, ops...)
}

// keyArg is the argument naming one of a list's items.
func keyArg(l List) string {
	if l.Key == "" {
		return l.Arg
	}
	return l.Key
}

// addBack is the add that puts a removed item back at its index.
func (c *decision) addBack(l List, key string) map[string]any {
	doc := asDoc(*c.manifest())
	i := l.find(doc, key)
	item := l.items(doc)[i]
	args := map[string]any{"index": i}
	if l.Key == "" {
		args[l.Arg] = item
		return args
	}
	for k, v := range item.(map[string]any) {
		args[k] = v
	}
	return args
}

// updateInverse is the update that sets an item back: what the patch
// changed, by a merge patch, and its old key.
func (c *decision) updateInverse(l List, u Updated) map[string]any {
	doc := asDoc(*c.manifest())
	before := l.items(doc)[l.find(doc, u.Key)]
	after := Merge(before, u.Patch)
	inv, _ := unmerge(before, after).(map[string]any)
	if inv == nil {
		inv = map[string]any{}
	}
	delete(inv, l.Key)
	inv[l.Key] = l.keyOf(after)
	if to := l.keyOf(after); to != u.Key {
		inv["rename"] = u.Key
	}
	return inv
}

// unmerge is the merge patch that takes after back to before: keys after
// added are null, changed keys are set back (objects key by key), and
// unchanged keys are left out.
func unmerge(before, after any) any {
	b, okB := before.(map[string]any)
	a, okA := after.(map[string]any)
	if !okB || !okA {
		return before
	}
	out := map[string]any{}
	for k := range a {
		if _, ok := b[k]; !ok {
			out[k] = nil
		}
	}
	for k, v := range b {
		if w, ok := a[k]; !ok || !equal(v, w) {
			out[k] = unmerge(v, w)
		}
	}
	return out
}
