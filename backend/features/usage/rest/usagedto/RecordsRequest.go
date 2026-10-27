package usagedto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

type RecordsRequest struct {
	From     time.Time `json:"from"` // RFC 3339; omitted leaves the range open
	To       time.Time `json:"to"`
	Feature  string    `json:"feature,omitempty"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize,omitempty"`
}

// ToArgs converts the request to the service's RecordsArgs.
func (r RecordsRequest) ToArgs(args features.ServiceArgs) usageservice.RecordsArgs {
	return usageservice.RecordsArgs{ServiceArgs: args, From: r.From, To: r.To, Feature: r.Feature, Page: r.Page, PageSize: r.PageSize}
}
