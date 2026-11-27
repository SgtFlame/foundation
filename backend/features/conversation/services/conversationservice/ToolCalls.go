package conversationservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// ToolCalls is a tools.CallStore saving through svc (a proxy in a process
// without the tenant database, forge 21d-2), as the principal the call
// ran as.
func ToolCalls(svc Service) tools.CallStore { return calls{svc} }

type calls struct{ svc Service }

func (c calls) SaveCall(args features.ServiceArgs, rec *tools.ToolCall) error {
	resp := c.svc.SaveToolCall(SaveToolCallArgs{ServiceArgs: args, Call: *rec})
	if resp.Common.Code != 200 {
		return logging.Code(resp.Common.ErrorCode).Errorf("the tool call was not kept: %s", resp.Common.Message)
	}
	return nil
}
