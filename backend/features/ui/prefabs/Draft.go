// Package prefabs keeps prefabs editable by many people at once (forge
// 04a, documentation/ServerUI.md › Built in slice 25): a prefab's draft is
// an aggregate on the def stream, changed only by prefab.* commands and
// folded into the ui.prefab projection; prefab.publish commits the draft
// as a version, which the ui.prefabPublished projection keeps and the
// versions follower copies into the tenant database. The prefab.describe
// and prefab.list queries are what editors and the chat read.
package prefabs

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// TargetType is a prefab's target type on the def stream.
const TargetType = "prefab"

// Projector IDs.
const (
	DraftProjector     = "ui.prefab"          // each prefab's draft
	PublishedProjector = "ui.prefabPublished" // each prefab's last published version
)

// Draft is the ui.prefab projection's value: a prefab as it is being
// edited.
type Draft struct {
	Prefab    ui.Prefab `json:"prefab"`
	Rev       int       `json:"rev"`       // events folded; prefab.publish names the one its sender saw
	Published int       `json:"published"` // the last published version, 0 before the first
	// PublishedRev is the Rev the last version was published at, so an
	// editor can tell whether the draft has changed since.
	PublishedRev int       `json:"publishedRev"`
	UpdatedBy    uint64    `json:"updatedBy,string"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Events a prefab's commands commit.
const (
	EventCreated       = "created"       // {prefab}
	EventImported      = "imported"      // {prefab}: the whole definition replaced
	EventInfoSet       = "infoSet"       // Info
	EventInterfaceSet  = "interfaceSet"  // Interface
	EventNodeInserted  = "nodeInserted"  // Insert
	EventNodeUpdated   = "nodeUpdated"   // Update: only what changed, per prop, binding and handler
	EventNodeMoved     = "nodeMoved"     // Move
	EventNodeRemoved   = "nodeRemoved"   // {path}
	EventNodesWrapped  = "nodesWrapped"  // Wrap
	EventNodeUnwrapped = "nodeUnwrapped" // {path}
	EventGraphAttached = "graphAttached" // {name, graph}
	EventGraphDetached = "graphDetached" // {name}
	EventPreviewSet    = "previewSet"    // {props}
	EventVariantsSet   = "variantsSet"   // {variants}
	EventPublished     = "published"     // Publication
)

// Info changes a prefab's name, description and owner; nil leaves them.
type Info struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Owner       *string `json:"owner,omitempty"`
}

// Interface replaces the sections it gives; nil leaves them.
type Interface struct {
	Props   *map[string]ui.PropDecl  `json:"props,omitempty"`
	Events  *map[string]ui.EventDecl `json:"events,omitempty"`
	Slots   *map[string]ui.SlotDecl  `json:"slots,omitempty"`
	State   *map[string]ui.StateDecl `json:"state,omitempty"`
	Queries *map[string]ui.QueryDecl `json:"queries,omitempty"`
}

// Insert puts a node under the node at ParentPath (into its slot when
// Slot is set) at Index; with no ParentPath it becomes the root of an
// empty template.
type Insert struct {
	ParentPath string   `json:"parentPath,omitempty"`
	Slot       string   `json:"slot,omitempty"`
	Index      int      `json:"index"`
	Node       *ui.Node `json:"node"`
}

// Update changes one node: each prop, binding and handler given is set,
// and one given as null is cleared; one left out stays (forge 04a
// question 2). Key renames the node.
type Update struct {
	Path  string             `json:"path"`
	Props map[string]any     `json:"props,omitempty"`
	Bind  map[string]*string `json:"bind,omitempty"`
	On    map[string]*ui.On  `json:"on,omitempty"`
	Key   string             `json:"key,omitempty"`
	// Variant picks an instance's variant; "" clears it (null in the
	// command's args), and nil leaves it.
	Variant *string `json:"variant,omitempty"`
}

// Move puts the node at Path under the node at ParentPath (into its slot
// when Slot is set), at Index of that list once the node has left its
// place.
type Move struct {
	Path       string `json:"path"`
	ParentPath string `json:"parentPath"`
	Slot       string `json:"slot,omitempty"`
	Index      int    `json:"index"`
}

// Wrap puts sibling nodes, in their order, into Wrapper, which takes the
// first one's place.
type Wrap struct {
	Paths   []string `json:"paths"`
	Wrapper *ui.Node `json:"wrapper"`
}

// Publication is a published version.
type Publication struct {
	Version int       `json:"version"`
	Prefab  ui.Prefab `json:"prefab"`
	// Breaking are the interface changes since the previous version that
	// break published prefabs using this one, published anyway with
	// allowBreaking.
	Breaking []Break `json:"breaking,omitempty"`
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

// Apply folds one event into a draft. Commands also apply the events they
// decide to a copy, and check the result.
func Apply(d *Draft, e live.Event) error {
	p := &d.Prefab
	var err error
	switch e.Type {
	case EventCreated:
		var v struct{ Prefab ui.Prefab }
		if v, err = decode[struct{ Prefab ui.Prefab }](e.Data); err == nil {
			*p = v.Prefab
		}
	case EventImported:
		var v struct{ Prefab ui.Prefab }
		if v, err = decode[struct{ Prefab ui.Prefab }](e.Data); err == nil {
			version := p.Version
			*p = v.Prefab
			p.Version = version
		}
	case EventInfoSet:
		var in Info
		if in, err = decode[Info](e.Data); err == nil {
			if in.Name != nil {
				p.Name = *in.Name
			}
			if in.Description != nil {
				p.Description = *in.Description
			}
			if in.Owner != nil {
				p.Owner = *in.Owner
			}
		}
	case EventInterfaceSet:
		var in Interface
		if in, err = decode[Interface](e.Data); err == nil {
			if in.Props != nil {
				p.Props = *in.Props
			}
			if in.Events != nil {
				p.Events = *in.Events
			}
			if in.Slots != nil {
				p.Slots = *in.Slots
			}
			if in.State != nil {
				p.State = *in.State
			}
			if in.Queries != nil {
				p.Queries = *in.Queries
			}
		}
	case EventNodeInserted:
		var in Insert
		if in, err = decode[Insert](e.Data); err == nil {
			err = insert(p, in)
		}
	case EventNodeUpdated:
		var in Update
		if in, err = decode[Update](e.Data); err == nil {
			err = update(p, in)
		}
	case EventNodeMoved:
		var in Move
		if in, err = decode[Move](e.Data); err == nil {
			err = move(p, in)
		}
	case EventNodeRemoved:
		var in struct{ Path string }
		if in, err = decode[struct{ Path string }](e.Data); err == nil {
			_, err = take(p, in.Path)
		}
	case EventNodesWrapped:
		var in Wrap
		if in, err = decode[Wrap](e.Data); err == nil {
			err = wrap(p, in)
		}
	case EventNodeUnwrapped:
		var in struct{ Path string }
		if in, err = decode[struct{ Path string }](e.Data); err == nil {
			err = unwrap(p, in.Path)
		}
	case EventGraphAttached:
		var in struct{ Name, Graph string }
		if in, err = decode[struct{ Name, Graph string }](e.Data); err == nil {
			if p.Graphs == nil {
				p.Graphs = map[string]string{}
			}
			p.Graphs[in.Name] = in.Graph
		}
	case EventGraphDetached:
		var in struct{ Name string }
		if in, err = decode[struct{ Name string }](e.Data); err == nil {
			delete(p.Graphs, in.Name)
		}
	case EventVariantsSet:
		var in struct{ Variants map[string]ui.VariantDecl }
		if in, err = decode[struct{ Variants map[string]ui.VariantDecl }](e.Data); err == nil {
			p.Variants = in.Variants
			if len(p.Variants) == 0 {
				p.Variants = nil
			}
		}
	case EventPreviewSet:
		var in struct{ Props map[string]any }
		if in, err = decode[struct{ Props map[string]any }](e.Data); err == nil {
			p.Preview.Props = in.Props
		}
	case EventPublished:
		var pub Publication
		if pub, err = decode[Publication](e.Data); err == nil {
			d.Published, d.PublishedRev, p.Version = pub.Version, d.Rev+1, pub.Version
		}
	default:
		return fmt.Errorf("unknown event %s", e.Type)
	}
	if err != nil {
		return err
	}
	d.Rev++
	d.UpdatedBy, d.UpdatedAt = e.UserID, e.Time
	p.ID = e.Target.ID
	normalize(p)
	return nil
}

// normalize gives a prefab's sections empty maps rather than null.
func normalize(p *ui.Prefab) {
	if p.Props == nil {
		p.Props = map[string]ui.PropDecl{}
	}
	if p.State == nil {
		p.State = map[string]ui.StateDecl{}
	}
	if p.Events == nil {
		p.Events = map[string]ui.EventDecl{}
	}
	if p.Slots == nil {
		p.Slots = map[string]ui.SlotDecl{}
	}
	if p.Queries == nil {
		p.Queries = map[string]ui.QueryDecl{}
	}
	if p.Graphs == nil {
		p.Graphs = map[string]string{}
	}
}

// fold is the ui.prefab projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	var d Draft
	if f.State != nil {
		if err := json.Unmarshal(f.State, &d); err != nil {
			return nil, err
		}
	}
	if err := Apply(&d, f.Event); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

// foldPublished is the ui.prefabPublished projector's fold: the last
// published version.
func foldPublished(f live.Fold) (json.RawMessage, error) {
	if f.Event.Type != EventPublished {
		return f.State, nil
	}
	p, err := decode[Publication](f.Event.Data)
	if err != nil {
		return nil, err
	}
	p.Prefab.ID, p.Prefab.Version = f.Event.Target.ID, p.Version
	return json.Marshal(p.Prefab)
}

// The template's nodes are found by key paths from the root, joined with
// '/' (root/row/title), so a path stays valid while other nodes move.

// place is where a node sits: in its parent's children, or in one of its
// parent's slots. The root has no parent.
type place struct {
	parent *ui.Node
	slot   string
	index  int
	node   *ui.Node
}

func (pl place) list() []*ui.Node {
	if pl.parent == nil {
		return nil
	}
	if pl.slot == "" {
		return pl.parent.Children
	}
	return pl.parent.Slots[pl.slot]
}

func setList(parent *ui.Node, slot string, list []*ui.Node) {
	if slot == "" {
		parent.Children = list
		return
	}
	if parent.Slots == nil {
		parent.Slots = map[string][]*ui.Node{}
	}
	if len(list) == 0 {
		delete(parent.Slots, slot)
		if len(parent.Slots) == 0 {
			parent.Slots = nil
		}
		return
	}
	parent.Slots[slot] = list
}

// find is the place of the node at path.
func find(root *ui.Node, path string) (place, bool) {
	keys := strings.Split(path, "/")
	if root == nil || path == "" || keys[0] != root.Key {
		return place{}, false
	}
	at := place{node: root}
	for _, k := range keys[1:] {
		next, ok := childPlace(at.node, k)
		if !ok {
			return place{}, false
		}
		at = next
	}
	return at, true
}

// childPlace finds a child of n by key, among its children and then its
// slots' content.
func childPlace(n *ui.Node, key string) (place, bool) {
	for i, c := range n.Children {
		if c.Key == key {
			return place{parent: n, index: i, node: c}, true
		}
	}
	for _, s := range slices.Sorted(maps.Keys(n.Slots)) {
		for i, c := range n.Slots[s] {
			if c.Key == key {
				return place{parent: n, slot: s, index: i, node: c}, true
			}
		}
	}
	return place{}, false
}

func noNode(path string) error { return fmt.Errorf("there is no node at %q", path) }

// put inserts node into parent's list (a slot's when slot is set) at
// index; an index out of range appends.
func put(parent *ui.Node, slot string, index int, node *ui.Node) {
	list := slices.Clone(parentList(parent, slot))
	if index < 0 || index > len(list) {
		index = len(list)
	}
	setList(parent, slot, slices.Insert(list, index, node))
}

func parentList(parent *ui.Node, slot string) []*ui.Node {
	if slot == "" {
		return parent.Children
	}
	return parent.Slots[slot]
}

func insert(p *ui.Prefab, in Insert) error {
	if in.ParentPath == "" {
		if p.Template != nil {
			return fmt.Errorf("the template already has a root")
		}
		p.Template = in.Node
		return nil
	}
	at, ok := find(p.Template, in.ParentPath)
	if !ok {
		return noNode(in.ParentPath)
	}
	put(at.node, in.Slot, in.Index, in.Node)
	return nil
}

// take removes the node at path and returns it.
func take(p *ui.Prefab, path string) (*ui.Node, error) {
	at, ok := find(p.Template, path)
	if !ok {
		return nil, noNode(path)
	}
	if at.parent == nil {
		p.Template = nil
		return at.node, nil
	}
	setList(at.parent, at.slot, slices.Delete(slices.Clone(at.list()), at.index, at.index+1))
	return at.node, nil
}

func update(p *ui.Prefab, in Update) error {
	at, ok := find(p.Template, in.Path)
	if !ok {
		return noNode(in.Path)
	}
	n := at.node
	for k, v := range in.Props {
		if v == nil {
			delete(n.Props, k)
			continue
		}
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		n.Props[k] = v
	}
	for k, v := range in.Bind {
		if v == nil {
			delete(n.Bind, k)
			continue
		}
		if n.Bind == nil {
			n.Bind = map[string]string{}
		}
		n.Bind[k] = *v
	}
	for k, v := range in.On {
		if v == nil {
			delete(n.On, k)
			continue
		}
		if n.On == nil {
			n.On = map[string]ui.On{}
		}
		n.On[k] = *v
	}
	if len(n.Props) == 0 {
		n.Props = nil
	}
	if len(n.Bind) == 0 {
		n.Bind = nil
	}
	if len(n.On) == 0 {
		n.On = nil
	}
	if in.Key != "" {
		n.Key = in.Key
	}
	if in.Variant != nil {
		n.Variant = *in.Variant
	}
	return nil
}

func move(p *ui.Prefab, in Move) error {
	n, err := take(p, in.Path)
	if err != nil {
		return err
	}
	at, ok := find(p.Template, in.ParentPath)
	if !ok {
		return noNode(in.ParentPath)
	}
	put(at.node, in.Slot, in.Index, n)
	return nil
}

func wrap(p *ui.Prefab, in Wrap) error {
	var first place
	var nodes []*ui.Node
	for i, path := range in.Paths {
		at, ok := find(p.Template, path)
		if !ok {
			return noNode(path)
		}
		if i == 0 || at.index < first.index {
			first = at
		}
		nodes = append(nodes, at.node)
	}
	if first.parent == nil {
		if len(nodes) > 1 {
			return fmt.Errorf("the root cannot be wrapped with siblings")
		}
		w := in.Wrapper.Clone()
		w.Children = []*ui.Node{first.node}
		p.Template = w
		return nil
	}
	keep := slices.DeleteFunc(slices.Clone(first.list()), func(n *ui.Node) bool { return slices.Contains(nodes, n) })
	// The wrapped nodes keep the order they had
	order := slices.Clone(nodes)
	list := first.list()
	slices.SortFunc(order, func(a, b *ui.Node) int { return slices.Index(list, a) - slices.Index(list, b) })
	w := in.Wrapper.Clone()
	w.Children = append(w.Children, order...)
	idx := min(first.index, len(keep))
	setList(first.parent, first.slot, slices.Insert(keep, idx, w))
	return nil
}

func unwrap(p *ui.Prefab, path string) error {
	at, ok := find(p.Template, path)
	if !ok {
		return noNode(path)
	}
	if at.parent == nil {
		if len(at.node.Children) != 1 {
			return fmt.Errorf("the root can be unwrapped only when it has one child")
		}
		p.Template = at.node.Children[0]
		return nil
	}
	list := slices.Delete(slices.Clone(at.list()), at.index, at.index+1)
	setList(at.parent, at.slot, slices.Insert(list, at.index, at.node.Children...))
	return nil
}
