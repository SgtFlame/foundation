package impl

import (
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
)

var log = ui.Log.Tags("session")

type service struct {
	reg   *extensions.Registry
	bus   *bus.Bus
	subs  *subscribe.Subscriptions
	store ui.SessionStore

	mu       sync.Mutex
	sessions map[string]*ui.Session // open in this instance
}

type Option func(*service)

// WithRegistry gives the components, apps, pages and editor kinds.
func WithRegistry(reg *extensions.Registry) Option { return func(s *service) { s.reg = reg } }

// WithBus decides the handlers' commands.
func WithBus(b *bus.Bus) Option { return func(s *service) { s.bus = b } }

// WithSubscriptions opens the pages' queries.
func WithSubscriptions(subs *subscribe.Subscriptions) Option {
	return func(s *service) { s.subs = subs }
}

// WithStore keeps the sessions' records (ui.KVSessions in serve).
func WithStore(store ui.SessionStore) Option { return func(s *service) { s.store = store } }

var (
	instance uiservice.Service
	once     sync.Once
)

// CreateService returns the UI service, built on first call.
func CreateService(opts ...Option) uiservice.Service {
	once.Do(func() { instance = New(opts...) })
	return instance
}

// New builds a service that is not the singleton, for tests.
func New(opts ...Option) uiservice.Service {
	s := &service{sessions: map[string]*ui.Session{}}
	for _, o := range opts {
		o(s)
	}
	go s.touch()
	return s
}

// touch stores every open session's record again before it ages out.
func (s *service) touch() {
	tick := time.NewTicker(ui.SessionRefresh)
	defer tick.Stop()
	for range tick.C {
		s.mu.Lock()
		open := make([]*ui.Session, 0, len(s.sessions))
		for _, sess := range s.sessions {
			open = append(open, sess)
		}
		s.mu.Unlock()
		for _, sess := range open {
			sess.Touch()
		}
	}
}

