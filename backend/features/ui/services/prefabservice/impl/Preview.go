package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/prefabservice"
)

// Preview evaluates a prefab once with props, reading its queries once.
func (s *service) Preview(args prefabservice.PreviewArgs) prefabservice.PreviewResponse {
	if failed := s.check(args.ServiceArgs, args.Prefab); failed != nil {
		return prefabservice.PreviewResponse{Common: *failed}
	}
	tree, problems, fail := ui.Preview(args.ServiceArgs, ui.Options{Catalog: ui.Catalog(s.reg), Subs: s.subs}, args.Prefab, args.Props, args.Draft)
	if fail != nil {
		return prefabservice.PreviewResponse{Common: *fail}
	}
	return prefabservice.PreviewResponse{Common: features.OK(), Tree: tree, Problems: problems}
}
