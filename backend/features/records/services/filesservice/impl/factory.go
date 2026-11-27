package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/files"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice"
)

var log = records.Log.Tags("filesservice")

type service struct {
	store *files.Store
}

type Option func(*service)

// WithStore is the object stores the service keeps.
func WithStore(st *files.Store) Option { return func(s *service) { s.store = st } }

var (
	instance filesservice.Service
	once     sync.Once
)

// CreateService returns the files service, built on first call.
func CreateService(opts ...Option) filesservice.Service {
	once.Do(func() { instance = New(opts...) })
	return instance
}

// New builds a service that is not the singleton, for tests.
func New(opts ...Option) filesservice.Service {
	s := &service{}
	for _, o := range opts {
		o(s)
	}
	return s
}
