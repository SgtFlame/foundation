package authz

import (
	"context"

	"github.com/nats-io/nats.go"
	"gorm.io/gorm"
)

// Source is where a Policy reads grants and custom roles: the application
// database in the process that holds it, authz's data service elsewhere
// (authzservice.Remote, forge 21d-1).
type Source interface {
	// CustomRoles is every tenant's custom roles.
	CustomRoles(ctx context.Context) ([]CustomRole, error)
	// Grants is a user's (or, with agent, an agent's) grants by id,
	// expired ones included.
	Grants(ctx context.Context, agent bool, id uint64) ([]RoleGrant, error)
}

type dbSource struct{ db *gorm.DB }

func (s dbSource) CustomRoles(ctx context.Context) ([]CustomRole, error) {
	var custom []CustomRole
	err := s.db.WithContext(ctx).Find(&custom).Error
	return custom, err
}

func (s dbSource) Grants(ctx context.Context, agent bool, id uint64) ([]RoleGrant, error) {
	var rows []RoleGrant
	q := s.db.WithContext(ctx).Where("user_id = ? AND agent_id = 0", id)
	if agent {
		q = s.db.WithContext(ctx).Where("agent_id = ?", id)
	}
	err := q.Order("id").Find(&rows).Error
	return rows, err
}

// ReadSource reads grants and roles from the policy's own Source, for
// authz's data service to serve them.
func (pol *Policy) ReadSource() Source { return pol.source }

// ChangedSubject is where a change to grants or roles is announced to
// every process's policy (forge 21d-1).
const ChangedSubject = "svc.authz.changed"

// Broadcast makes Changed announce itself on nc, so every policy that
// Follows drops its caches.
func (pol *Policy) Broadcast(nc *nats.Conn) {
	pol.notify = func() {
		if err := nc.Publish(ChangedSubject, nil); err != nil {
			log.Warn(context.Background(), "authz: the change was not announced: %v", err)
		}
	}
}

// Follow drops the policy's caches whenever any process announces a
// change, and after NATS reconnects (a change may have been missed).
func (pol *Policy) Follow(nc *nats.Conn) (*nats.Subscription, error) {
	nc.SetReconnectHandler(chainReconnect(nc, pol.drop))
	sub, err := nc.Subscribe(ChangedSubject, func(*nats.Msg) { pol.drop() })
	if err != nil {
		return nil, log.Wrap(err)
	}
	return sub, nil
}

// chainReconnect keeps nc's existing reconnect handler and runs fn too.
func chainReconnect(nc *nats.Conn, fn func()) nats.ConnHandler {
	prev := nc.Opts.ReconnectedCB
	return func(c *nats.Conn) {
		if prev != nil {
			prev(c)
		}
		fn()
	}
}
