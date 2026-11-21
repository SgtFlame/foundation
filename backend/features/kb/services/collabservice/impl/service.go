package impl

import (
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

type service struct {
	root        string
	gracePeriod time.Duration // how long an empty room is kept, so a reload rejoins it
	events      volumeservice.FileEvents

	mu     sync.Mutex
	rooms  map[uint64]*room
	byPath map[string]*room // by document key: a file's absolute path, doc:<key> for others
	kinds  func(kind string) (collabservice.DocumentKind, bool)
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
		delete(s.byPath, r.doc.key())
		r.drop()
	}
}

func (s *service) rel(p string) string {
	return volumeservice.Rel(s.root, p)
}

var log = volumeservice.Log.Tags("collab")
