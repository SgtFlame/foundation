package logsdto

import (
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type StatusResponse struct {
	rest.Status
	Configured   bool       `json:"configured"`
	URL          string     `json:"url,omitempty"`
	StoredLevel  string     `json:"storedLevel"`
	BrowserLevel string     `json:"browserLevel"`
	Queued       int        `json:"queued"`
	Dropped      int64      `json:"dropped"`
	LastError    string     `json:"lastError,omitempty"`
	LastErrorAt  *time.Time `json:"lastErrorAt,omitempty"`
}

func StatusResponseFrom(resp logsservice.StatusResponse) StatusResponse {
	out := StatusResponse{
		Status: rest.StatusFrom(resp.Common), Configured: resp.Configured, URL: resp.URL,
		StoredLevel: strings.ToLower(resp.StoredLevel.String()), BrowserLevel: strings.ToLower(resp.BrowserLevel.String()),
		Queued: resp.Queued, Dropped: resp.Dropped, LastError: resp.LastError,
	}
	if !resp.LastErrorAt.IsZero() {
		out.LastErrorAt = &resp.LastErrorAt
	}
	return out
}
