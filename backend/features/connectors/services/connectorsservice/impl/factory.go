package impl

import (
	"net/http"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("connectors").Tags("connectorsservice")

type service struct {
	js    jetstream.JetStream
	books connectors.Books
}

type Option func(*service)

// WithJetStream is where the books are kept: the services account's.
func WithJetStream(js jetstream.JetStream) Option {
	return func(s *service) { s.js, s.books = js, connectors.InJetStream(js) }
}

var (
	instance connectorsservice.Service
	once     sync.Once
)

// CreateService returns the connectors service, built on first call.
func CreateService(opts ...Option) connectorsservice.Service {
	once.Do(func() { instance = New(opts...) })
	return instance
}

// New builds a service that is not the singleton, for tests.
func New(opts ...Option) connectorsservice.Service {
	s := &service{}
	for _, o := range opts {
		o(s)
	}
	return s
}

// check refuses anyone but a service, and answers 503 without a store.
func (s *service) check(args features.ServiceArgs) *features.ServiceResponse {
	if args.Principal.Kind != features.PrincipalService {
		return args.Fail(http.StatusForbidden, log.Code("services_only").New("only services may keep connectors' books"))
	}
	if s.books == nil {
		return args.Fail(http.StatusServiceUnavailable, log.Code("connectors_unavailable").New("the connectors' books are not kept here"))
	}
	return nil
}

func (s *service) done(args features.ServiceArgs, err error) features.ServiceResponse {
	if err != nil {
		return *args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	return features.OK()
}
