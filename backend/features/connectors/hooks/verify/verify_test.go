package verify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// Every scheme accepts what it signed, and refuses another body, another
// secret, a missing header and (where it signs a time) an old one.
func TestSchemes(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	body := []byte(`{"id":"evt_1","type":"invoice.paid"}`)
	for _, scheme := range []string{Stripe, GitHub, StandardWebhooks, HMACSHA256} {
		secret := "s3cret"
		if scheme == StandardWebhooks {
			secret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("a key of some length"))
		}
		h := Sign(scheme, secret, body, now)
		if err := Verify(scheme, secret, h, body, now.Add(time.Minute)); err != nil {
			t.Errorf("%s refused its own signature: %v", scheme, err)
		}
		if err := Verify(scheme, secret, h, []byte(`{"id":"evt_2"}`), now); !errors.Is(err, ErrSignature) {
			t.Errorf("%s took another body: %v", scheme, err)
		}
		if err := Verify(scheme, "other", h, body, now); !errors.Is(err, ErrSignature) {
			t.Errorf("%s took another secret: %v", scheme, err)
		}
		if err := Verify(scheme, secret, http.Header{}, body, now); !errors.Is(err, ErrSignature) {
			t.Errorf("%s took no signature: %v", scheme, err)
		}
		if err := Verify(scheme, "", h, body, now); !errors.Is(err, ErrSignature) {
			t.Errorf("%s took no secret: %v", scheme, err)
		}
		late := Verify(scheme, secret, h, body, now.Add(6*time.Minute))
		if scheme == GitHub {
			if late != nil {
				t.Errorf("github signs no time but refused a late delivery: %v", late)
			}
		} else if !errors.Is(late, ErrExpired) {
			t.Errorf("%s took a signature six minutes old: %v", scheme, late)
		}
	}
}

// The signatures are the services' own: computed here by their documented
// recipes, independent of Sign.
func TestDocumentedRecipes(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"ok":true}`)
	hm := func(key []byte, msg string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(msg))
		return m.Sum(nil)
	}
	// Stripe: with a second, stale v1 while the secret rolls
	h := http.Header{}
	h.Set(StripeHeader, "t="+ts+",v1=00ff,v1="+hex.EncodeToString(hm([]byte("whsec_abc"), ts+"."+string(body))))
	if err := Verify(Stripe, "whsec_abc", h, body, now); err != nil {
		t.Errorf("stripe: %v", err)
	}
	// GitHub
	h = http.Header{}
	h.Set(GitHubHeader, "sha256="+hex.EncodeToString(hm([]byte("gh"), string(body))))
	if err := Verify(GitHub, "gh", h, body, now); err != nil {
		t.Errorf("github: %v", err)
	}
	// Standard Webhooks, with two signatures, the second the right one
	key := []byte("0123456789abcdef")
	h = http.Header{}
	h.Set(WebhookID, "msg_1")
	h.Set(WebhookTime, ts)
	h.Set(WebhookSig, "v1,AAAA v1,"+base64.StdEncoding.EncodeToString(hm(key, "msg_1."+ts+"."+string(body))))
	if err := Verify(StandardWebhooks, "whsec_"+base64.StdEncoding.EncodeToString(key), h, body, now); err != nil {
		t.Errorf("standard webhooks: %v", err)
	}
	// The generic scheme without a timestamp
	h = http.Header{}
	h.Set(SignatureHeader, "sha256="+hex.EncodeToString(hm([]byte("k"), string(body))))
	if err := Verify(HMACSHA256, "k", h, body, now.Add(time.Hour)); err != nil {
		t.Errorf("hmac-sha256: %v", err)
	}
}
