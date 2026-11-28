package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// bucket is a hook's token bucket as cx_hook_rates keeps it.
type bucket struct {
	Tokens float64   `json:"tokens"`
	At     time.Time `json:"at"`
}

// take takes one token from a hook's bucket, refilled at rate a second up
// to burst, by compare-and-set so every process shares it. It reports
// whether the request may go on and, when not, how long until a token is
// back. Contention past a few tries lets the request through rather than
// refusing a delivery the limit did not decide.
func take(ctx context.Context, kv jetstream.KeyValue, id string, rate, burst float64, now time.Time) (bool, time.Duration, error) {
	if rate <= 0 {
		return true, 0, nil
	}
	for try := 0; try < 5; try++ {
		var b bucket
		var rev uint64
		e, err := kv.Get(ctx, id)
		switch {
		case errors.Is(err, jetstream.ErrKeyNotFound):
			b = bucket{Tokens: burst, At: now}
		case err != nil:
			return false, 0, err
		default:
			rev = e.Revision()
			if json.Unmarshal(e.Value(), &b) != nil {
				b = bucket{Tokens: burst, At: now}
			}
		}
		if dt := now.Sub(b.At).Seconds(); dt > 0 {
			b.Tokens = math.Min(burst, b.Tokens+dt*rate)
		}
		b.At = now
		allowed := b.Tokens >= 1
		if allowed {
			b.Tokens--
		}
		raw, _ := json.Marshal(b)
		if rev == 0 {
			_, err = kv.Create(ctx, id, raw)
		} else {
			_, err = kv.Update(ctx, id, raw, rev)
		}
		if err == nil {
			if allowed {
				return true, 0, nil
			}
			return false, time.Duration((1 - b.Tokens) / rate * float64(time.Second)), nil
		}
		if !errors.Is(err, jetstream.ErrKeyExists) && !isWrongLast(err) {
			return false, 0, err
		}
	}
	return true, 0, nil
}

func isWrongLast(err error) bool {
	var apiErr *jetstream.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence
}
