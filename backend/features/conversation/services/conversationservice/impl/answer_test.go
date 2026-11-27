package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// A round waits in one process; the browser's answer reaches another
// (forge 21c).
func TestAnswerReachesAnotherProcess(t *testing.T) {
	ns := natsd.StartTest(t)
	keys, err := services.NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	verify := services.VerifyWith(keys.Public())
	waiter := newService(WithCalls(&services.Client{NC: ns.Conn(), Caller: "a", Keys: keys}, verify))
	other := newService(WithCalls(&services.Client{NC: ns.Conn(), Caller: "b", Keys: keys}, verify))

	owner := features.LocalPrincipal()
	w := waiter.expect(owner, 42, "call.1")
	defer waiter.forget(42, "call.1")
	answer := func(p features.Principal, call string) conversationservice.AnswerResponse {
		return other.Answer(conversationservice.AnswerArgs{
			ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: p},
			RoundID:     42, CallID: call, Approval: tools.ApprovalApproved, Result: json.RawMessage(`{"ok":true}`),
		})
	}

	stranger := features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: owner.TenantID}
	if r := answer(stranger, "call.1"); r.Common.Code != http.StatusNotFound {
		t.Fatalf("another user's answer %+v", r.Common)
	}
	if r := answer(owner, "call.2"); r.Common.Code != http.StatusNotFound || r.Common.ErrorCode != "call_not_waiting" {
		t.Fatalf("an answer nobody waits for %+v", r.Common)
	}
	if r := answer(owner, "call.1"); r.Common.Code != http.StatusOK {
		t.Fatalf("the owner's answer %+v", r.Common)
	}
	select {
	case a := <-w.answers:
		if a.Approval != tools.ApprovalApproved || string(a.Result) != `{"ok":true}` || a.Principal.UserID != owner.UserID {
			t.Fatalf("answer %+v", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the answer never arrived")
	}
}
