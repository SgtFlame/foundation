package embeddto

import (
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type DescribeResponse struct {
	rest.Status
	Model      string `json:"model,omitempty"`
	Dimensions int    `json:"dimensions,omitempty"`
	MaxBatch   int    `json:"maxBatch,omitempty"`
}

// DescribeResponseFrom converts the service's DescribeResponse; its code is
// carried by the HTTP status rather than the body.
func DescribeResponseFrom(resp embedservice.DescribeResponse) DescribeResponse {
	return DescribeResponse{Status: rest.StatusFrom(resp.Common), Model: resp.Model, Dimensions: resp.Dimensions, MaxBatch: resp.MaxBatch}
}
