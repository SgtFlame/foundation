package impl

import "github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"

// Event runs a handler of an open session; the session sends its patches.
func (s *service) Event(args uiservice.EventArgs) {
	args.Session.Event(args.ServiceArgs, args.Handler, args.Path, args.Payload)
}
