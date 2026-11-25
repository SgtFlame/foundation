package conversation

import "github.com/SgtFlame/foundation/backend/features"

// WorkspaceKnowledge is what a chat round takes from the workspace it was
// sent from (forge 16c): the Volumes its retrieval leaves out and the
// Chronicle entries it always uses.
type WorkspaceKnowledge struct {
	ID              string
	Name            string
	ExceptVolumeIDs []uint64 // the Volumes unchecked there
	CheckedEntries  []string // the Chronicle entries checked there, as ids
}

// WorkspaceReader reads one of the caller's own workspaces (the id
// "default" is their Default, which a person who never opened the
// workbench may not have yet: false for it gives the defaults). It is
// wired by serve.go to the workspace feature
// (conversationimpl.WithWorkspaces), so the browser names a workspace and
// the server reads its lists.
type WorkspaceReader func(args features.ServiceArgs, id string) (WorkspaceKnowledge, bool)

// DefaultWorkspace is the id of everyone's Default workspace.
const DefaultWorkspace = "default"

// ChronicleID is the Chronicle context provider's id (chroniclecontext.ID),
// whose checked entries a round takes from its workspace.
const ChronicleID = "chronicle"
