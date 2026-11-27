// Package vaulttest is an in-memory stand-in for the parts of Vault's HTTP
// API the server uses, for tests.
package vaulttest

import (
	"encoding/json"
	"fmt"
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
	unwraps  map[string]string // wrapping token → the SecretID it wraps
	roles    map[string]*Role
	secrets  map[string]*secretID // SecretID → its role and accessor
	policies map[string]string
	n        int
	kv       map[string]map[string]any
	versions map[string]int
	Down     bool // every request fails with 503
}

// New starts a fake Vault, closed when the test ends.
func New(t *testing.T) *Fake {
	f := &Fake{Token: "server-token", wrapped: map[string]string{}, unwraps: map[string]string{}, roles: map[string]*Role{}, secrets: map[string]*secretID{},
		policies: map[string]string{}, kv: map[string]map[string]any{}, versions: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

// Role is an AppRole made through the API (forge 20h).
type Role struct {
	Name     string
	RoleID   string
	Policies []string
}

type secretID struct {
	role, accessor string
	uses           int
}

// Roles is a copy of the AppRoles made through the API.
func (f *Fake) Roles() map[string]Role {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]Role{}
	for n, r := range f.roles {
		out[n] = *r
	}
	return out
}

// Policy is an ACL policy's text.
func (f *Fake) Policy(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.policies[name]
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
		sid := "secret-id"
		if s, ok := f.unwraps[token]; ok {
			sid = s
			delete(f.unwraps, token)
		}
		reply(map[string]any{"data": map[string]any{"secret_id": sid}})
	case path == "auth/approle/login":
		if body["role_id"] == "role-id" && body["secret_id"] == "secret-id" {
			reply(map[string]any{"auth": map[string]any{"client_token": f.Token, "lease_duration": 3600}})
			return
		}
		s, ok := f.secrets[str(body["secret_id"])]
		if !ok || f.roles[s.role] == nil || f.roles[s.role].RoleID != body["role_id"] {
			w.WriteHeader(http.StatusBadRequest)
			reply(map[string]any{"errors": []string{"invalid role or secret ID"}})
			return
		}
		if s.uses--; s.uses <= 0 {
			delete(f.secrets, str(body["secret_id"]))
		}
		reply(map[string]any{"auth": map[string]any{"client_token": "token-" + s.role, "lease_duration": 3600, "policies": f.roles[s.role].Policies}})
	case !authed:
		w.WriteHeader(http.StatusForbidden)
	case strings.HasPrefix(path, "sys/policies/acl/") && r.Method == http.MethodPut:
		f.policies[strings.TrimPrefix(path, "sys/policies/acl/")] = str(body["policy"])
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(path, "auth/approle/role/"):
		f.approle(w, r, strings.TrimPrefix(path, "auth/approle/role/"), body, reply)
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

func str(v any) string {
	s, _ := v.(string)
	return s
}

func (f *Fake) approle(w http.ResponseWriter, r *http.Request, rest string, body map[string]any, reply func(any)) {
	name, sub, _ := strings.Cut(rest, "/")
	role := f.roles[name]
	switch {
	case sub == "" && r.Method == http.MethodPost:
		if role == nil {
			f.n++
			role = &Role{Name: name, RoleID: fmt.Sprintf("role-%s-%d", name, f.n)}
			f.roles[name] = role
		}
		role.Policies = nil
		if ps, ok := body["token_policies"].([]any); ok {
			for _, p := range ps {
				role.Policies = append(role.Policies, str(p))
			}
		}
		w.WriteHeader(http.StatusNoContent)
	case role == nil:
		w.WriteHeader(http.StatusNotFound)
	case sub == "role-id":
		reply(map[string]any{"data": map[string]any{"role_id": role.RoleID}})
	case sub == "secret-id" && r.Method == http.MethodPost:
		f.n++
		sid, acc := fmt.Sprintf("sid-%d", f.n), fmt.Sprintf("acc-%d", f.n)
		f.secrets[sid] = &secretID{role: name, accessor: acc, uses: 1}
		if r.Header.Get("X-Vault-Wrap-TTL") == "" {
			reply(map[string]any{"data": map[string]any{"secret_id": sid, "secret_id_accessor": acc}})
			return
		}
		token := fmt.Sprintf("wrap-%d", f.n)
		cp := "auth/approle/role/" + name + "/secret-id"
		f.wrapped[token] = cp
		f.unwraps[token] = sid
		reply(map[string]any{"wrap_info": map[string]any{"token": token, "accessor": "wacc-" + token, "ttl": 60, "creation_path": cp, "wrapped_accessor": acc}})
	case sub == "secret-id-accessor/destroy":
		for id, s := range f.secrets {
			if s.accessor == str(body["secret_id_accessor"]) && s.role == name {
				delete(f.secrets, id)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		w.WriteHeader(http.StatusBadRequest)
		reply(map[string]any{"errors": []string{"failed to find accessor entry"}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}
