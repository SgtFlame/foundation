// Package routes keeps services.Routes current from KV svc_routes (forge
// 21f-2): <service>.t<tenant> → the suffix of the servers dedicated to
// that tenant's calls. 22i's tenant overrides write it; Set is the same
// for tests and tools.
package routes

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("services")

// Bucket is the routes' KV bucket.
const Bucket = "svc_routes"

// Open is the routes' bucket, made on first use.
func Open(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: Bucket, History: 1}))
	return kv, log.Wrap(err)
}

// Watch fills r from the bucket and keeps it current until ctx ends or
// stop is called; it returns once the current routes are in r.
func Watch(ctx context.Context, js jetstream.JetStream, r *services.Routes) (stop func(), err error) {
	kv, err := Open(ctx, js)
	if err != nil {
		return nil, err
	}
	wctx, cancel := context.WithCancel(ctx)
	w, err := kv.WatchAll(wctx)
	if err != nil {
		cancel()
		return nil, log.Wrap(err)
	}
	ready := make(chan struct{})
	go func() {
		defer func() { _ = w.Stop() }()
		initial := true
		for {
			select {
			case <-wctx.Done():
				if initial {
					close(ready)
				}
				return
			case e, ok := <-w.Updates():
				if !ok {
					if initial {
						close(ready)
					}
					return
				}
				if e == nil {
					if initial {
						initial = false
						close(ready)
					}
					continue
				}
				service, tenant, ok := parse(e.Key())
				if !ok {
					continue
				}
				if e.Operation() == jetstream.KeyValuePut {
					r.Set(service, tenant, string(e.Value()))
				} else {
					r.Set(service, tenant, "")
				}
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
	}
	return cancel, nil
}

// Set routes tenant's calls of service to the servers started for suffix
// (services.ForSuffix); an empty suffix sends them back to the shared ones.
func Set(ctx context.Context, js jetstream.JetStream, service string, tenant uint64, suffix string) error {
	kv, err := Open(ctx, js)
	if err != nil {
		return err
	}
	key := services.RouteKey(service, tenant)
	if suffix == "" {
		err = kv.Delete(ctx, key)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			err = nil
		}
		return log.Wrap(err)
	}
	_, err = kv.PutString(ctx, key, services.SubjectToken(suffix))
	return log.Wrap(err)
}

func parse(key string) (string, uint64, bool) {
	i := strings.LastIndex(key, ".t")
	if i <= 0 {
		return "", 0, false
	}
	t, err := strconv.ParseUint(key[i+2:], 10, 64)
	return key[:i], t, err == nil && t != 0
}
