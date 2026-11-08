package prefabs

import (
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// undo sets a prefab.* command's inverse (forge 06a): the ops that take
// the draft back to the state the command was decided against, read from
// that state. Nodes are undone by key path, node by node, so undoing a
// change leaves what others changed in other nodes since.
func (c *decision) undo(command string, events []live.NewEvent) {
	if c.Op == nil {
		return
	}
	id := c.Target.ID
	before := c.prefab()
	app := appOf(before.Owner)
	op := func(command string, args any) live.Op { return live.OpOf(command, id, args) }
	switch command {
	case "prefab.publish":
		c.NoUndo(app, "a published version stays; publish the draft again to replace it")
		return
	case "prefab.create":
		a, _ := args[struct{ Owner string }](c)
		c.NoUndo(appOf(a.Owner), "a new prefab cannot be deleted yet")
		return
	case "prefab.import":
		if !c.exists {
			a, _ := args[struct{ Prefab *ui.Prefab }](c)
			owner := ""
			if a.Prefab != nil {
				owner = a.Prefab.Owner
			}
			c.NoUndo(appOf(owner), "a new prefab cannot be deleted yet")
			return
		}
		p := *before
		p.Version = 0
		c.Undo(app, op("prefab.import", map[string]any{"prefab": p}))
		return
	}
	var ops []live.Op
	for _, e := range events {
		var inv []live.Op
		switch d := e.Data.(type) {
		case Info:
			var in Info
			if d.Name != nil {
				in.Name = &before.Name
			}
			if d.Description != nil {
				in.Description = &before.Description
			}
			if d.Owner != nil {
				in.Owner = &before.Owner
				if app == "" {
					app = appOf(*d.Owner)
				}
			}
			inv = []live.Op{op("prefab.setInfo", in)}
		case Interface:
			var in Interface
			if d.Props != nil {
				in.Props = &before.Props
			}
			if d.Events != nil {
				in.Events = &before.Events
			}
			if d.Slots != nil {
				in.Slots = &before.Slots
			}
			if d.State != nil {
				in.State = &before.State
			}
			if d.Queries != nil {
				in.Queries = &before.Queries
			}
			inv = []live.Op{op("prefab.setInterface", in)}
		case Insert:
			path := d.Node.Key
			if d.ParentPath != "" {
				path = d.ParentPath + "/" + path
			}
			inv = []live.Op{op("prefab.removeNode", map[string]any{"path": path})}
		case Update:
			inv = []live.Op{op("prefab.updateNode", c.updateInverse(d))}
		case Move:
			inv = c.moveInverse(d)
		case Wrap:
			inv = c.wrapInverse(d)
		case map[string]any:
			inv = c.mapInverse(e.Type, d)
		}
		if inv == nil {
			c.NoUndo(app, "its inverse cannot be read")
			return
		}
		ops = append(inv, ops...)
	}
	c.Undo(app, ops...)
}

// mapInverse inverts the events whose data is a map.
func (c *decision) mapInverse(event string, d map[string]any) []live.Op {
	id := c.Target.ID
	p := c.prefab()
	switch event {
	case EventNodeRemoved:
		path, _ := d["path"].(string)
		at, ok := find(p.Template, path)
		if !ok {
			return nil
		}
		in := map[string]any{"node": at.node}
		if at.parent != nil {
			in["parentPath"], in["index"] = parentPath(path), at.index
			if at.slot != "" {
				in["slot"] = at.slot
			}
		}
		return []live.Op{live.OpOf("prefab.insertNode", id, in)}
	case EventNodeUnwrapped:
		path, _ := d["path"].(string)
		at, ok := find(p.Template, path)
		if !ok {
			return nil
		}
		wrapper := at.node.Clone()
		wrapper.Children = nil
		if len(at.node.Children) == 0 {
			// Nothing to wrap: the node comes back where it was
			return c.mapInverse(EventNodeRemoved, d)
		}
		under := parentPath(path)
		var paths []string
		for _, ch := range at.node.Children {
			paths = append(paths, strings.TrimPrefix(under+"/"+ch.Key, "/"))
		}
		return []live.Op{live.OpOf("prefab.wrapNodes", id, Wrap{Paths: paths, Wrapper: wrapper})}
	case EventGraphAttached:
		name, _ := d["name"].(string)
		if old, ok := p.Graphs[name]; ok {
			return []live.Op{live.OpOf("prefab.attachGraph", id, map[string]any{"name": name, "graph": old})}
		}
		return []live.Op{live.OpOf("prefab.detachGraph", id, map[string]any{"name": name})}
	case EventGraphDetached:
		name, _ := d["name"].(string)
		return []live.Op{live.OpOf("prefab.attachGraph", id, map[string]any{"name": name, "graph": p.Graphs[name]})}
	case EventPreviewSet:
		props := p.Preview.Props
		if props == nil {
			props = map[string]any{}
		}
		return []live.Op{live.OpOf("prefab.setPreview", id, map[string]any{"props": props})}
	case EventVariantsSet:
		variants := p.Variants
		if variants == nil {
			variants = map[string]ui.VariantDecl{}
		}
		return []live.Op{live.OpOf("prefab.setVariants", id, map[string]any{"variants": variants})}
	}
	return nil
}

// updateInverse sets back what an update changes: each prop, binding and
// handler it sets or clears, the node's key and its variant.
func (c *decision) updateInverse(u Update) map[string]any {
	at, _ := find(c.prefab().Template, u.Path)
	n := at.node
	path := u.Path
	inv := map[string]any{}
	if u.Key != "" {
		path = strings.TrimPrefix(parentPath(u.Path)+"/"+u.Key, "/")
		inv["key"] = n.Key
	}
	inv["path"] = path
	if len(u.Props) > 0 {
		props := map[string]any{}
		for k := range u.Props {
			props[k] = n.Props[k] // nil, which clears, when it had none
		}
		inv["props"] = props
	}
	if len(u.Bind) > 0 {
		bind := map[string]any{}
		for k := range u.Bind {
			if v, ok := n.Bind[k]; ok {
				bind[k] = v
			} else {
				bind[k] = nil
			}
		}
		inv["bind"] = bind
	}
	if len(u.On) > 0 {
		on := map[string]any{}
		for k := range u.On {
			if v, ok := n.On[k]; ok {
				on[k] = v
			} else {
				on[k] = nil
			}
		}
		inv["on"] = on
	}
	if u.Variant != nil {
		if n.Variant == "" {
			inv["variant"] = nil
		} else {
			inv["variant"] = n.Variant
		}
	}
	return inv
}

// moveInverse moves the node back to its parent, slot and index.
func (c *decision) moveInverse(m Move) []live.Op {
	at, ok := find(c.prefab().Template, m.Path)
	if !ok || at.parent == nil {
		return nil
	}
	back := map[string]any{"path": m.ParentPath + "/" + at.node.Key, "parentPath": parentPath(m.Path), "index": at.index}
	if at.slot != "" {
		back["slot"] = at.slot
	}
	return []live.Op{live.OpOf("prefab.moveNode", c.Target.ID, back)}
}

// wrapInverse unwraps the wrapper, then puts the wrapped nodes back where
// they were when they were not side by side: each goes to the end of its
// list, then to its old index, lowest first, which no later move shifts.
func (c *decision) wrapInverse(w Wrap) []live.Op {
	id := c.Target.ID
	first, ok := find(c.prefab().Template, w.Paths[0])
	if !ok {
		return nil
	}
	if first.parent == nil {
		return []live.Op{live.OpOf("prefab.unwrapNode", id, map[string]any{"path": w.Wrapper.Key})}
	}
	type was struct {
		key   string
		index int
	}
	var wrapped []was
	for _, path := range w.Paths {
		at, ok := find(c.prefab().Template, path)
		if !ok {
			return nil
		}
		wrapped = append(wrapped, was{at.node.Key, at.index})
	}
	slices.SortFunc(wrapped, func(a, b was) int { return a.index - b.index })
	under := parentPath(w.Paths[0])
	ops := []live.Op{live.OpOf("prefab.unwrapNode", id, map[string]any{"path": under + "/" + w.Wrapper.Key})}
	together := true
	for i := range wrapped {
		together = together && wrapped[i].index == wrapped[0].index+i
	}
	if together {
		return ops
	}
	move := func(key string, index *int) live.Op {
		m := map[string]any{"path": under + "/" + key, "parentPath": under}
		if first.slot != "" {
			m["slot"] = first.slot
		}
		if index != nil {
			m["index"] = *index
		}
		return live.OpOf("prefab.moveNode", id, m)
	}
	for _, x := range wrapped {
		ops = append(ops, move(x.key, nil))
	}
	for _, x := range wrapped {
		ops = append(ops, move(x.key, &x.index))
	}
	return ops
}

// parentPath is the path of a node's parent; "" for the root.
func parentPath(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

// appOf is the app an owner names: app:<appId>.
func appOf(owner string) string {
	app, ok := strings.CutPrefix(owner, "app:")
	if !ok {
		return ""
	}
	return app
}
