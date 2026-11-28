// Package verify checks the signatures of callbacks services push to a
// hook (forge 26b-1), one function per scheme a type's Receives names:
// stripe, github, standard-webhooks and hmac-sha256. A type's Receive
// calls Verify with its connection's signing secret before it reads the
// body. Sign makes a signed request for tests, as each service would.
package verify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Tolerance is how far a signed timestamp may be from now (Stripe's and
// Standard Webhooks' five minutes), against replays.
const Tolerance = 5 * time.Minute

// The schemes, as connectors.Receivers names them.
const (
	Stripe           = "stripe"
	GitHub           = "github"
	StandardWebhooks = "standard-webhooks"
	HMACSHA256       = "hmac-sha256"
)

// The headers each scheme reads.
const (
	StripeHeader    = "Stripe-Signature"
	GitHubHeader    = "X-Hub-Signature-256"
	WebhookID       = "Webhook-Id"
	WebhookTime     = "Webhook-Timestamp"
	WebhookSig      = "Webhook-Signature"
	SignatureHeader = "X-Signature-256"
	TimestampHeader = "X-Signature-Timestamp"
)

// ErrSignature is a signature that does not match, or is missing.
var ErrSignature = errors.New("the signature does not match")

// ErrExpired is a signed timestamp outside the tolerance.
var ErrExpired = errors.New("the signature's time is too far from now")

// Verify checks a callback's headers and body against the secret, at now.
func Verify(scheme, secret string, h http.Header, body []byte, now time.Time) error {
	if secret == "" {
		return fmt.Errorf("no signing secret is set: %w", ErrSignature)
	}
	switch scheme {
	case Stripe:
		return stripe(secret, h, body, now)
	case GitHub:
		return github(secret, h, body)
	case StandardWebhooks:
		return standard(secret, h, body, now)
	case HMACSHA256:
		return generic(secret, h, body, now)
	}
	return fmt.Errorf("%q is not a callback scheme", scheme)
}

// Sign gives the headers a service would send with body at now.
func Sign(scheme, secret string, body []byte, now time.Time) http.Header {
	h := http.Header{}
	ts := strconv.FormatInt(now.Unix(), 10)
	switch scheme {
	case Stripe:
		h.Set(StripeHeader, "t="+ts+",v1="+hexMAC([]byte(secret), ts+"."+string(body)))
	case GitHub:
		h.Set(GitHubHeader, "sha256="+hexMAC([]byte(secret), string(body)))
	case StandardWebhooks:
		id := "msg_" + hexMAC([]byte(secret), string(body))[:16]
		h.Set(WebhookID, id)
		h.Set(WebhookTime, ts)
		h.Set(WebhookSig, "v1,"+base64.StdEncoding.EncodeToString(mac(standardKey(secret), id+"."+ts+"."+string(body))))
	case HMACSHA256:
		h.Set(TimestampHeader, ts)
		h.Set(SignatureHeader, "sha256="+hexMAC([]byte(secret), ts+"."+string(body)))
	}
	return h
}

func mac(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func hexMAC(key []byte, msg string) string { return hex.EncodeToString(mac(key, msg)) }

func equalHex(want []byte, got string) bool {
	b, err := hex.DecodeString(got)
	return err == nil && hmac.Equal(want, b)
}

func fresh(ts string, now time.Time) error {
	n, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
	if err != nil {
		return fmt.Errorf("the timestamp is not readable: %w", ErrSignature)
	}
	if d := now.Sub(time.Unix(n, 0)); d > Tolerance || d < -Tolerance {
		return ErrExpired
	}
	return nil
}

// stripe: Stripe-Signature t=<unix>,v1=<hex HMAC of "t.body">, maybe
// several v1 while a secret rolls.
func stripe(secret string, h http.Header, body []byte, now time.Time) error {
	var ts string
	var sigs []string
	for _, part := range strings.Split(h.Get(StripeHeader), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if ts == "" || len(sigs) == 0 {
		return ErrSignature
	}
	want := mac([]byte(secret), ts+"."+string(body))
	for _, s := range sigs {
		if equalHex(want, s) {
			return fresh(ts, now)
		}
	}
	return ErrSignature
}

// github: X-Hub-Signature-256 sha256=<hex HMAC of the body>. GitHub signs
// no time; its delivery id makes a repeat one event.
func github(secret string, h http.Header, body []byte) error {
	sig, ok := strings.CutPrefix(h.Get(GitHubHeader), "sha256=")
	if !ok || !equalHex(mac([]byte(secret), string(body)), sig) {
		return ErrSignature
	}
	return nil
}

// standardKey is a Standard Webhooks secret's key: whsec_<base64>, else
// the secret's bytes.
func standardKey(secret string) []byte {
	if b64, ok := strings.CutPrefix(secret, "whsec_"); ok {
		if key, err := base64.StdEncoding.DecodeString(b64); err == nil {
			return key
		}
	}
	return []byte(secret)
}

// standard: Webhook-Id, Webhook-Timestamp and Webhook-Signature
// "v1,<base64 HMAC of id.timestamp.body>", space separated when several.
func standard(secret string, h http.Header, body []byte, now time.Time) error {
	id, ts := h.Get(WebhookID), h.Get(WebhookTime)
	if id == "" || ts == "" {
		return ErrSignature
	}
	want := mac(standardKey(secret), id+"."+ts+"."+string(body))
	for _, s := range strings.Fields(h.Get(WebhookSig)) {
		v, sig, ok := strings.Cut(s, ",")
		if !ok || v != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(sig)
		if err == nil && hmac.Equal(want, got) {
			return fresh(ts, now)
		}
	}
	return ErrSignature
}

// generic: X-Signature-256 sha256=<hex HMAC>, of "timestamp.body" when
// X-Signature-Timestamp is sent (then checked against the tolerance), else
// of the body.
func generic(secret string, h http.Header, body []byte, now time.Time) error {
	sig, ok := strings.CutPrefix(h.Get(SignatureHeader), "sha256=")
	if !ok {
		return ErrSignature
	}
	ts := h.Get(TimestampHeader)
	msg := string(body)
	if ts != "" {
		msg = ts + "." + msg
	}
	if !equalHex(mac([]byte(secret), msg), sig) {
		return ErrSignature
	}
	if ts != "" {
		return fresh(ts, now)
	}
	return nil
}
