package embeddto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
)

type DescribeRequest struct {
}

// ToArgs converts the request to the service's DescribeArgs.
func (r DescribeRequest) ToArgs(args features.ServiceArgs) embedservice.DescribeArgs {
	return embedservice.DescribeArgs{ServiceArgs: args}
}
