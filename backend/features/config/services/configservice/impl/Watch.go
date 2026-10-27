package impl

import (
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

// Watch delivers each stored change until args' context ends. A watcher
// that falls behind misses events rather than blocking the publisher.
func (s *service) Watch(args configservice.WatchArgs) *configservice.WatchRequest {
	events := make(chan config.Changed, 16)
	if s.changes == nil {
		close(events)
		return &configservice.WatchRequest{Events: events}
	}
	ctx := args.Context()
	done := make(chan struct{})
	unsubscribe := s.changes.Subscribe(func(c config.Changed) {
		select {
		case <-done:
		case events <- c:
		default:
		}
	})
	go func() {
		<-ctx.Done()
		unsubscribe()
		close(done)
		close(events)
	}()
	return &configservice.WatchRequest{Events: events}
}
