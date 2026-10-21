package impl

import (
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type service struct {
	root        string
	gracePeriod time.Duration // how long an empty room is kept, so a reload rejoins it

	mu     sync.Mutex
	rooms  map[uint64]*room
	byPath map[string]*room
}

func (s *service) room(id uint64) *room {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rooms[id]
}

// dropRoom forgets r unless a client rejoined it meanwhile.
func (s *service) dropRoom(r *room) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.empty() && s.rooms[r.id] == r {
		delete(s.rooms, r.id)
		delete(s.byPath, r.file)
	}
}

func (s *service) rel(p string) string {
	return volumeservice.Rel(s.root, p)
}
