package tools

import "github.com/SgtFlame/foundation/backend/features"

// CallStore keeps the ToolCall records a Loop makes (forge 21d-2): the
// conversation data service's, through conversationservice.ToolCalls in
// any process, or the database itself in conversation's own packages
// (conversation.CallsIn). A record has its ID before it is saved, so a
// retried save stores it once. args are the run's, its principal the
// record's owner.
type CallStore interface {
	SaveCall(args features.ServiceArgs, rec *ToolCall) error
}
