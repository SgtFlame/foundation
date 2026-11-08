package entities

import (
	"encoding/json"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// undo sets an entity.* command's inverse (forge 06a): the ops that take
// the draft back to the state the command was decided against, read from
// that state, field by field. Fields are never deleted, so undoing a new
// field removes it the way entity.removeField does: hidden, with no values.
func (c *decision) undo(command string, events []live.NewEvent) {
	if c.Op == nil {
		return
	}
	id := c.Target.ID
	x := c.entity()
	app := appOf(x.Owner)
	op := func(command string, args any) live.Op { return live.OpOf(command, id, args) }
	switch command {
	case "entity.publish":
		c.NoUndo(app, "a published version stays, and records follow it; publish the draft again to replace it")
		return
	case "entity.create", "entity.import":
		if !c.exists {
			var a struct {
				Owner  string
				Entity struct{ Owner string }
			}
			_ = json.Unmarshal(c.Args, &a)
			c.Undo(appOf(a.Owner+a.Entity.Owner), op("entity.delete", map[string]any{}))
			return
		}
		e := *x
		e.Version = 0
		c.Undo(app, op("entity.import", map[string]any{"entity": e}))
		return
	case "entity.delete":
		c.Undo(app, op("entity.restore", map[string]any{}))
		return
	case "entity.restore":
		c.Undo(app, op("entity.delete", map[string]any{}))
		return
	}
	var ops []live.Op
	for _, e := range events {
		var inv live.Op
		switch d := e.Data.(type) {
		case Info:
			var in Info
			if d.Name != nil {
				in.Name = &x.Name
			}
			if d.Description != nil {
				in.Description = &x.Description
			}
			if d.Owner != nil {
				in.Owner = &x.Owner
				if app == "" {
					app = appOf(*d.Owner)
				}
			}
			if d.TitleField != nil {
				in.TitleField = &x.TitleField
			}
			inv = op("entity.setInfo", in)
		case map[string]any:
			switch e.Type {
			case EventFieldAdded:
				f, _ := d["field"].(records.Field)
				inv = op("entity.removeField", map[string]any{"field": f.ID})
			case EventFieldUpdated:
				f, _ := d["field"].(records.Field)
				inv = op("entity.updateField", c.fieldInverse(f))
			case EventFieldRemoved:
				inv = op("entity.restoreField", map[string]any{"field": d["field"]})
			case EventFieldRestored:
				inv = op("entity.removeField", map[string]any{"field": d["field"]})
			case EventIndexAdded:
				i, _ := d["index"].(records.Index)
				inv = op("entity.removeIndex", map[string]any{"index": i.ID})
			case EventIndexRemoved:
				for _, i := range x.Indexes {
					if i.ID == d["index"] {
						inv = op("entity.addIndex", i)
					}
				}
			case EventAccessSet:
				inv = op("entity.setAccess", map[string]any{"access": x.Access})
			}
		}
		if inv.Command == "" {
			c.NoUndo(app, "its inverse cannot be read")
			return
		}
		ops = append([]live.Op{inv}, ops...)
	}
	c.Undo(app, ops...)
}

// fieldInverse is the patch that sets a field back as it was: every
// property it had, and null for those the change added.
func (c *decision) fieldInverse(now records.Field) map[string]any {
	old, _, _ := c.field(now.ID)
	var was, is map[string]any
	raw, _ := json.Marshal(old)
	_ = json.Unmarshal(raw, &was)
	raw, _ = json.Marshal(now)
	_ = json.Unmarshal(raw, &is)
	patch := map[string]any{}
	for k, v := range was {
		patch[k] = v
	}
	for k := range is {
		if _, ok := was[k]; !ok {
			patch[k] = nil
		}
	}
	delete(patch, "id")
	delete(patch, "hidden")
	patch["field"] = now.ID
	return patch
}

// appOf is the app an owner names: app:<appId>.
func appOf(owner string) string {
	app, ok := strings.CutPrefix(owner, "app:")
	if !ok {
		return ""
	}
	return app
}
