package usagedto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

type CaptureResponse struct {
	rest.Status
	RecordID uint64            `json:"recordId,string,omitempty"`
	Time     time.Time         `json:"time,omitzero"`
	System   string            `json:"system,omitempty"`
	Messages []CapturedMessage `json:"messages,omitempty"`
	Reply    string            `json:"reply,omitempty"`
}

type CapturedMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CaptureResponseFrom converts the service's CaptureResponse; its code is
// carried by the HTTP status rather than the body.
func CaptureResponseFrom(resp usageservice.CaptureResponse) CaptureResponse {
	out := CaptureResponse{Status: rest.StatusFrom(resp.Common)}
	c := resp.Capture
	if c.RecordID == 0 {
		return out
	}
	out.RecordID, out.Time, out.System, out.Reply = c.RecordID, c.CreatedAt, c.System, c.Reply
	_ = json.Unmarshal([]byte(c.Messages), &out.Messages)
	return out
}
