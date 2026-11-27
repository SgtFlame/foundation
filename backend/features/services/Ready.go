package services

import (
	"context"
	"slices"
	"time"

	"github.com/nats-io/nats.go"
)

// PingOp is the operation every server answers at once, before any token
// check (forge 21g-1): svc.<service>.ping says a server is there. Ops are
// capitalized, so it names no service's operation.
const PingOp = "ping"

// Ping reports whether some server of service answers within d.
func Ping(nc *nats.Conn, service string, d time.Duration) bool {
	_, err := nc.Request(Subject(service, PingOp), nil, d)
	return err == nil
}

// Missing are the services of names no server answers.
func Missing(nc *nats.Conn, names []string, d time.Duration) []string {
	var out []string
	for _, n := range names {
		if !Ping(nc, n, d) {
			out = append(out, n)
		}
	}
	return out
}

// WaitReady waits until a server of every service in names answers, or
// ctx ends; waiting is called every `every` with the services still
// missing, for the process's log.
func WaitReady(ctx context.Context, nc *nats.Conn, names []string, every time.Duration, waiting func(missing []string)) error {
	names = slices.Compact(slices.Sorted(slices.Values(names)))
	last := time.Now()
	for {
		missing := Missing(nc, names, time.Second)
		if len(missing) == 0 {
			return nil
		}
		if waiting != nil && time.Since(last) >= every {
			waiting(missing)
			last = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
