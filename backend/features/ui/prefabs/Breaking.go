package prefabs

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Break is a change to a prefab's interface that breaks its users: a
// prop, event, slot or variant removed, or a prop's type changed (forge
// 04a question 4, 04f).
type Break struct {
	Change string `json:"change"` // in words, e.g. "removing prop task"
	Path   string `json:"path"`   // within the definition, e.g. props.task
	UsedBy []Use  `json:"usedBy,omitempty"`
	kind   string // prop, event, slot or variant
	name   string
}

// Use is a published prefab's node that uses the changed part.
type Use struct {
	Prefab string `json:"prefab"`
	Path   string `json:"path"` // template.<node path>
}

// Breaking are the breaking changes from prev to next.
func Breaking(prev, next ui.Prefab) []Break {
	var out []Break
	for _, k := range slices.Sorted(mapKeys(prev.Props)) {
		old := prev.Props[k]
		now, ok := next.Props[k]
		switch {
		case !ok:
			out = append(out, Break{Change: "removing prop " + k, Path: "props." + k, kind: "prop", name: k})
		case now.Type != old.Type || now.Entity != old.Entity:
			out = append(out, Break{Change: fmt.Sprintf("changing prop %s from %s to %s", k, typeName(old), typeName(now)), Path: "props." + k + ".type", kind: "prop", name: k})
		}
	}
	for _, k := range slices.Sorted(mapKeys(prev.Events)) {
		if _, ok := next.Events[k]; !ok {
			out = append(out, Break{Change: "removing event " + k, Path: "events." + k, kind: "event", name: k})
		}
	}
	for _, k := range slices.Sorted(mapKeys(prev.Slots)) {
		if _, ok := next.Slots[k]; !ok {
			out = append(out, Break{Change: "removing slot " + k, Path: "slots." + k, kind: "slot", name: k})
		}
	}
	for _, k := range slices.Sorted(mapKeys(prev.Variants)) {
		if _, ok := next.Variants[k]; !ok {
			out = append(out, Break{Change: "removing variant " + k, Path: "variants." + k, kind: "variant", name: k})
		}
	}
	return out
}

func typeName(d ui.PropDecl) string {
	if d.Entity != "" {
		return d.Type + " of " + d.Entity
	}
	return d.Type
}

// uses reports whether an instance node uses what a break changes.
func (b Break) uses(n *ui.Node) bool {
	switch b.kind {
	case "prop":
		_, set := n.Props[b.name]
		_, bound := n.Bind[b.name]
		return set || bound
	case "event":
		_, ok := n.On[b.name]
		return ok
	case "slot":
		_, ok := n.Slots[b.name]
		return ok
	case "variant":
		return n.Variant == b.name
	}
	return false
}

// usedBy fills in the published prefabs each change breaks, and keeps the
// changes that break any.
func (c *decision) usedBy(changes []Break) []Break {
	if len(changes) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()
	entries, err := c.deps.Projections.Watch(ctx, c.Principal.TenantID, PublishedProjector, TargetType+".*")
	if err != nil {
		return nil
	}
	typ := ui.PrefabPrefix + c.Target.ID
	for e := range entries {
		if e == nil {
			break
		}
		var p ui.Prefab
		if e.Deleted || json.Unmarshal(e.Value.Value, &p) != nil || p.ID == c.Target.ID {
			continue
		}
		for _, path := range Paths(p.Template) {
			at, _ := find(p.Template, path)
			if at.node == nil || at.node.Type != typ {
				continue
			}
			for i := range changes {
				if changes[i].uses(at.node) {
					changes[i].UsedBy = append(changes[i].UsedBy, Use{Prefab: p.ID, Path: "template." + path})
				}
			}
		}
	}
	return slices.DeleteFunc(changes, func(b Break) bool { return len(b.UsedBy) == 0 })
}

// appVersions is the projection of published app versions
// (apps/manifests' VersionProjector), read here to find the apps using a
// prefab before it is deleted.
const appVersions = "apps.version"

// users are the published prefabs whose last version renders the target
// prefab, and the apps whose last published version pins it, named for a
// refused prefab.delete (forge 06b).
func (c *decision) users() []string {
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()
	var out []string
	if entries, err := c.deps.Projections.Watch(ctx, c.Principal.TenantID, PublishedProjector, TargetType+".*"); err == nil {
		for e := range entries {
			if e == nil {
				break
			}
			var p ui.Prefab
			if e.Deleted || json.Unmarshal(e.Value.Value, &p) != nil || p.ID == c.Target.ID {
				continue
			}
			for _, used := range ui.PrefabsUsed(p.Template) {
				if used == c.Target.ID {
					out = append(out, "the prefab "+p.ID)
					break
				}
			}
		}
	}
	last := map[string]apps.Version{}
	if entries, err := c.deps.Projections.Watch(ctx, c.Principal.TenantID, appVersions, ">"); err == nil {
		for e := range entries {
			if e == nil {
				break
			}
			var v apps.Version
			if e.Deleted || json.Unmarshal(e.Value.Value, &v) != nil {
				continue
			}
			if v.Version > last[v.App].Version {
				last[v.App] = v
			}
		}
	}
	for _, app := range slices.Sorted(maps.Keys(last)) {
		if _, ok := last[app].Prefabs[c.Target.ID]; ok {
			out = append(out, "the app "+app)
		}
	}
	return out
}
