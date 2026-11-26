package impl

import "github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"

// Close ends a session; a closed tab forgets it.
func (s *service) Close(args uiservice.CloseArgs) {
	s.mu.Lock()
	if s.sessions[args.Session.ID()] == args.Session {
		delete(s.sessions, args.Session.ID())
	}
	s.mu.Unlock()
	args.Session.Close(args.Forget)
}
