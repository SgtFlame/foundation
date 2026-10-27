package secrets

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/SgtFlame/foundation/backend/features/rest"
)

// Gate is the server's handler from the moment it listens: until Open it
// serves sealed (the bootstrap routes, 503 for every other API route and a
// page for the browser), then the full router.
type Gate struct {
	boot    *Bootstrap
	sealed  http.Handler
	handler atomic.Pointer[http.Handler]
}

// NewGate serves sealed through sealed, a router with the bootstrap routes.
func NewGate(boot *Bootstrap, sealed http.Handler) *Gate {
	return &Gate{boot: boot, sealed: sealed}
}

// Open serves h from now on and marks startup done.
func (g *Gate) Open(h http.Handler) {
	g.handler.Store(&h)
	g.boot.Started()
}

func (g *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h := g.handler.Load(); h != nil {
		(*h).ServeHTTP(w, r)
		return
	}
	g.sealed.ServeHTTP(w, r)
}

// NotReady answers what the sealed router has no route for: 503 with
// server_sealed (or server_starting) under /api, and a page explaining the
// state anywhere else.
func NotReady(boot *Bootstrap) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := boot.State()
		code, message := "server_starting", "The server is starting"
		if state == Sealed {
			code, message = "server_sealed", "The server is sealed: an operator must unseal it with backend/scripts/secrets/unseal.sh"
		}
		w.Header().Set("Retry-After", "5")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(rest.Status{Message: message, ErrorCode: code})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(sealedPage))
	}
}

const sealedPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="5"><title>Server sealed</title>
<style>
:root { color-scheme: light dark; --bg: #fafafa; --fg: #222; --muted: #666 }
@media (prefers-color-scheme: dark) { :root { --bg: #161616; --fg: #eee; --muted: #aaa } }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: var(--bg); color: var(--fg); font: 16px/1.5 system-ui, sans-serif }
main { max-width: 32rem; padding: 1rem }
p { color: var(--muted) }
code { font-size: 0.9em }
</style></head>
<body><main><h1>The server is sealed</h1>
<p>It keeps its secrets in Vault and is waiting for an operator to unseal it with <code>backend/scripts/secrets/unseal.sh</code>. This page reloads by itself.</p>
</main></body></html>`
