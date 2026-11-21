package impl

import (
	"sync"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
)

var log = live.Log.Tags("connection")

// Limits per connection, until the live section sets them.
const (
	defaultMaxSubscriptions = 500
	defaultMaxCalls         = 20
)

type service struct {
	bus              *bus.Bus
	subs             *subscribe.Subscriptions
	presence         *presence.Presence
	reg              *extensions.Registry
	maxSubscriptions func() int
	maxCalls         int
}

type Option func(*service)

// WithBus decides the connections' commands.
func WithBus(b *bus.Bus) Option { return func(s *service) { s.bus = b } }

// WithSubscriptions opens their queries.
func WithSubscriptions(subs *subscribe.Subscriptions) Option {
	return func(s *service) { s.subs = subs }
}

// WithPresence keeps their presence.
func WithPresence(p *presence.Presence) Option { return func(s *service) { s.presence = p } }

// WithRegistry gives the calls and connection extensions.
func WithRegistry(reg *extensions.Registry) Option { return func(s *service) { s.reg = reg } }

// WithMaxSubscriptions reads how many subscriptions one connection may hold.
func WithMaxSubscriptions(fn func() int) Option { return func(s *service) { s.maxSubscriptions = fn } }

var (
	instance liveservice.Service
	once     sync.Once
)

// CreateService returns the live service, built on first call.
func CreateService(opts ...Option) liveservice.Service {
	once.Do(func() {
		s := &service{maxSubscriptions: func() int { return defaultMaxSubscriptions }, maxCalls: defaultMaxCalls}
		for _, o := range opts {
			o(s)
		}
		instance = s
	})
	return instance
}

// New builds a service that is not the singleton, for tests.
func New(opts ...Option) liveservice.Service {
	s := &service{maxSubscriptions: func() int { return defaultMaxSubscriptions }, maxCalls: defaultMaxCalls}
	for _, o := range opts {
		o(s)
	}
	return s
}
