// Package vaulttest is an in-memory stand-in for the parts of Vault's HTTP
// API the server uses, for tests.
package vaulttest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Fake is a Vault with one KV v2 mount ("foundation") and one AppRole
// ("foundation"), whose wrapping tokens are made with Wrap.
type Fake struct {
	URL   string
	Token string // the token a login returns

	mu       sync.Mutex
	wrapped  map[string]string // wrapping token → creation path
	kv       map[string]map[string]any
	versions map[string]int
	Down     bool // every request fails with 503
}

// New starts a fake Vault, closed when the test ends.
func New(t *testing.T) *Fake {
	f := &Fake{Token: "server-token", wrapped: map[string]string{}, kv: map[string]map[string]any{}, versions: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

// Wrap makes a single-use wrapping token, as if made at creationPath.
func (f *Fake) Wrap(token, creationPath string) {
	f.mu.Lock()
	f.wrapped[token] = creationPath
	f.mu.Unlock()
}

// Set stores a secret's value directly.
func (f *Fake) Set(path, value string) {
	f.mu.Lock()
	f.versions[path]++
	f.kv[path] = map[string]any{"value": value}
	f.mu.Unlock()
}

// Value is a secret's stored value.
func (f *Fake) Value(path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.kv[path]["value"].(string)
	return v, ok
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Down {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	authed := r.Header.Get("X-Vault-Token") == f.Token

	switch {
	case path == "sys/wrapping/lookup":
		cp, ok := f.wrapped[body["token"].(string)]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			reply(map[string]any{"errors": []string{"wrapping token is not valid or does not exist"}})
			return
		}
		reply(map[string]any{"data": map[string]any{"creation_path": cp}})
	case path == "sys/wrapping/unwrap":
		token := r.Header.Get("X-Vault-Token")
		if _, ok := f.wrapped[token]; !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		delete(f.wrapped, token) // single use
		reply(map[string]any{"data": map[string]any{"secret_id": "secret-id"}})
	case path == "auth/approle/login":
		if body["role_id"] != "role-id" || body["secret_id"] != "secret-id" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reply(map[string]any{"auth": map[string]any{"client_token": f.Token, "lease_duration": 3600}})
	case !authed:
		w.WriteHeader(http.StatusForbidden)
	case r.Method == "LIST" && strings.HasPrefix(path, "foundation/metadata/"):
		prefix := strings.TrimPrefix(path, "foundation/metadata/") + "/"
		seen := map[string]bool{}
		for p := range f.kv {
			if rest, ok := strings.CutPrefix(p, prefix); ok {
				if i := strings.Index(rest, "/"); i >= 0 {
					rest = rest[:i+1]
				}
				seen[rest] = true
			}
		}
		if len(seen) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		keys := []string{}
		for k := range seen {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		reply(map[string]any{"data": map[string]any{"keys": keys}})
	case strings.HasPrefix(path, "foundation/data/"):
		p := strings.TrimPrefix(path, "foundation/data/")
		switch r.Method {
		case http.MethodGet:
			data, ok := f.kv[p]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			reply(map[string]any{"data": map[string]any{"data": data, "metadata": map[string]any{"version": f.versions[p]}}})
		case http.MethodPost:
			f.versions[p]++
			f.kv[p] = body["data"].(map[string]any)
			reply(map[string]any{"data": map[string]any{"version": f.versions[p]}})
		case http.MethodDelete:
			delete(f.kv, p)
			w.WriteHeader(http.StatusNoContent)
		}
	case strings.HasPrefix(path, "foundation/metadata/"):
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}
