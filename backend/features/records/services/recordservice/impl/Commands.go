package impl

import (
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

// Create sends record.create.
func (s *service) Create(args recordservice.CreateArgs) recordservice.CommandResponse {
	return s.command(args.ServiceArgs, "", "record.create", args.ID, map[string]any{"entity": args.Entity, "values": args.Values})
}

// Update sends record.update.
func (s *service) Update(args recordservice.UpdateArgs) recordservice.CommandResponse {
	return s.command(args.ServiceArgs, "", "record.update", args.ID, map[string]any{"entity": args.Entity, "set": args.Set, "expect": args.Expect})
}

// Delete sends record.delete.
func (s *service) Delete(args recordservice.DeleteArgs) recordservice.CommandResponse {
	return s.command(args.ServiceArgs, "", "record.delete", args.ID, map[string]any{"entity": args.Entity, "expect": args.Expect})
}
