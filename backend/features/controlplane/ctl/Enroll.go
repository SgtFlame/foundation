package ctl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Enrollment (forge 20c-1): a one-time token, kept hashed in the control
// account's ctl_enroll with its target and expiry, is exchanged on
// ctl.enroll for a user JWT scoped to the target, signed for a key the
// daemon made and keeps.
const (
	EnrollBucket  = "ctl_enroll"
	EnrollSubject = "ctl.enroll"
)

// EnrollPermissions are the shared enroll user's: it may only ask.
var EnrollPermissions = natsauth.Permissions{Pub: []string{EnrollSubject}, Sub: []string{"_INBOX.>"}}

// DaemonPermissions are a daemon's user's: its own request subjects, its
// own events, its own desired state, facts and heartbeat, and nothing of
// any other daemon's or of a tenant.
func DaemonPermissions(t Target) natsauth.Permissions {
	k := t.Key()
	return natsauth.Permissions{
		Pub: []string{
			"ctl.evt." + k + ".>",
			"$KV." + FactsBucket + "." + k,
			"$KV." + HealthBucket + "." + k,
			"$JS.API.DIRECT.GET.KV_" + DesiredBucket + ".$KV." + DesiredBucket + "." + k,
			"$JS.API.STREAM.INFO.KV_" + DesiredBucket,
			"$JS.API.STREAM.INFO.KV_" + FactsBucket,
			"$JS.API.STREAM.INFO.KV_" + HealthBucket,
			"$JS.API.INFO",
		},
		Sub:       []string{t.Requests(), "_INBOX.>"},
		Responses: true,
	}
}

type token struct {
	Target    Target    `json:"target"`
	ExpiresAt time.Time `json:"expiresAt"`
	Spent     bool      `json:"spent"`
}

func tokenKey(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func (c *Control) enrollKV(ctx context.Context) (jetstream.KeyValue, error) {
	kv, err := c.JS.CreateOrUpdateKeyValue(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: EnrollBucket, Description: "Enrollment tokens, by their hash", History: 1, Storage: jetstream.FileStorage}))
	return kv, log.Wrap(err)
}

// MintToken makes a one-time enrollment token for a target, valid for ttl;
// only its hash is kept, so the token is returned once.
func (c *Control) MintToken(ctx context.Context, t Target, ttl time.Duration) (string, error) {
	kv, err := c.enrollKV(ctx)
	if err != nil {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", log.Wrap(err)
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	raw, _ := json.Marshal(token{Target: t, ExpiresAt: time.Now().Add(ttl).UTC()})
	if _, err := kv.Create(ctx, tokenKey(tok), raw); err != nil {
		return "", log.Wrap(err)
	}
	return tok, nil
}

// EnrollRequest is a daemon's: its token and the public key it made.
type EnrollRequest struct {
	Token     string `json:"token"`
	PublicKey string `json:"publicKey"`
}

// EnrollReply is a user JWT for that key, scoped to the target.
type EnrollReply struct {
	JWT    string `json:"jwt,omitempty"`
	Target Target `json:"target"`
	Error  string `json:"error,omitempty"`
}

// Signer signs a user JWT for a public key: the control account
// (*natsauth.Account).
type Signer interface {
	UserJWT(public, name string, p natsauth.Permissions, ttl time.Duration) (string, error)
}

// ServeEnroll answers ctl.enroll in a queue group shared by every
// process: a token is spent in the same KV update that checks it
// (expected revision), so it enrolls once.
func ServeEnroll(ctx context.Context, c *Control, signer Signer) (*nats.Subscription, error) {
	kv, err := c.enrollKV(ctx)
	if err != nil {
		return nil, err
	}
	sub, err := c.NC.QueueSubscribe(EnrollSubject, "ctl.enroll", func(m *nats.Msg) {
		reply := func(r EnrollReply) {
			raw, _ := json.Marshal(r)
			_ = m.Respond(raw)
		}
		var req EnrollRequest
		if err := json.Unmarshal(m.Data, &req); err != nil || req.Token == "" {
			reply(EnrollReply{Error: "bad request"})
			return
		}
		e, err := kv.Get(ctx, tokenKey(req.Token))
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			reply(EnrollReply{Error: "unknown token"})
			return
		}
		if err != nil {
			reply(EnrollReply{Error: "try again"})
			return
		}
		var t token
		_ = json.Unmarshal(e.Value(), &t)
		switch {
		case t.Spent:
			reply(EnrollReply{Error: "the token was used"})
			return
		case time.Now().After(t.ExpiresAt):
			reply(EnrollReply{Error: "the token expired"})
			return
		}
		t.Spent = true
		raw, _ := json.Marshal(t)
		if _, err := kv.Update(ctx, tokenKey(req.Token), raw, e.Revision()); err != nil {
			reply(EnrollReply{Error: "the token was used"})
			return
		}
		jwt, err := signer.UserJWT(req.PublicKey, "axiomd-"+t.Target.Key(), DaemonPermissions(t.Target), 0)
		if err != nil {
			reply(EnrollReply{Error: err.Error()})
			return
		}
		log.With("target", t.Target.Key()).Info(ctx, "controlplane: enrolled %s", t.Target.Key())
		reply(EnrollReply{JWT: jwt, Target: t.Target})
	})
	if err != nil {
		return nil, log.Wrap(err)
	}
	return sub, log.Wrap(c.NC.Flush())
}
