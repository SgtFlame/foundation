package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice"
	"github.com/SgtFlame/foundation/backend/features/text/services/parseservice"
)

var log = logging.In("parse")

type service struct {
	files    func() filesservice.Service
	maxBytes int
}

type Option func(*service)

// WithFiles is where uploaded files are read (the files service, through
// the locator); without it only inline text is parsed.
func WithFiles(files func() filesservice.Service) Option {
	return func(s *service) { s.files = files }
}

// WithMaxBytes bounds the files read; 64 MB when left out.
func WithMaxBytes(n int) Option { return func(s *service) { s.maxBytes = n } }

var (
	instance parseservice.Service
	once     sync.Once
)

// CreateService returns the parse service, built on first call.
func CreateService(opts ...Option) parseservice.Service {
	once.Do(func() { instance = New(opts...) })
	return instance
}

// New builds a service that is not the singleton, for tests.
func New(opts ...Option) parseservice.Service {
	s := &service{maxBytes: 64 << 20}
	for _, o := range opts {
		o(s)
	}
	return s
}
