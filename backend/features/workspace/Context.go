package workspace

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Kinds of item a workspace's context lists (forge 16c). Kinds are open:
// a later source (Blackboards, channels, Docs folders) adds its own.
const (
	KindVolume    = "volume"
	KindChronicle = "chronicle"
)

// Context is a workspace's active knowledge (forge 16c question 10): per
// kind, the items whose check differs from the kind's default, by id.
// Every item the person can read is listed in every workspace; a Volume is
// checked unless set false here, a Chronicle entry unchecked unless set
// true, so Default and a new workspace search what chat searched before.
type Context map[string]map[string]bool

// CheckedByDefault says whether a kind's items are checked when a
// workspace says nothing of them: Volumes are, entries and unknown kinds
// are not.
func CheckedByDefault(kind string) bool { return kind == KindVolume }

// ParseContext reads a workspace's stored context; nothing, or something
// that is not one, is the defaults.
func ParseContext(raw json.RawMessage) Context {
	c := Context{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c)
	}
	return c
}

// Checked says whether an item is checked in the context.
func (c Context) Checked(kind, id string) bool {
	if v, ok := c[kind][id]; ok {
		return v
	}
	return CheckedByDefault(kind)
}

// Differing are the ids of kind whose check is not the default (the
// unchecked Volumes, the checked entries), sorted.
func (c Context) Differing(kind string) []string {
	def := CheckedByDefault(kind)
	var out []string
	for id, on := range c[kind] {
		if on != def {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// IDs are Differing as numbers, leaving out any that is not one.
func (c Context) IDs(kind string) []uint64 {
	var out []uint64
	for _, id := range c.Differing(kind) {
		if n, err := strconv.ParseUint(id, 10, 64); err == nil && n != 0 {
			out = append(out, n)
		}
	}
	return out
}

// Get reads one of args' principal's workspaces, from their own only:
// false when they have none by that id (another person's id is none).
func Get(args features.ServiceArgs, store live.Store, id string) (Workspace, bool) {
	p := args.Principal
	if store == nil || p.Kind != features.PrincipalUser || p.UserID == 0 || !live.ValidToken(id) {
		return Workspace{}, false
	}
	v, ok, err := store.Get(args.Context(), p.TenantID, Projector, TypeOf(p.UserID)+"."+id)
	if err != nil || !ok {
		return Workspace{}, false
	}
	var w Workspace
	if json.Unmarshal(v.Value, &w) != nil || w.ID == "" {
		return Workspace{}, false
	}
	return w, true
}

// KnowledgeReader is how a chat round reads the workspace it was sent from
// (forge 16c, conversationimpl.WithWorkspaces): from the caller's own
// workspaces only, read when the round starts, deps late-bound since the
// conversation service is made before the projections.
func KnowledgeReader(deps func() Deps) conversation.WorkspaceReader {
	return func(args features.ServiceArgs, id string) (conversation.WorkspaceKnowledge, bool) {
		w, ok := Get(args, deps().Store, id)
		if !ok {
			return conversation.WorkspaceKnowledge{}, false
		}
		c := ParseContext(w.Context)
		return conversation.WorkspaceKnowledge{ID: w.ID, Name: w.Name, ExceptVolumeIDs: c.IDs(KindVolume), CheckedEntries: c.Differing(KindChronicle)}, true
	}
}
