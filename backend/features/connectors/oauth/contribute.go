package oauth

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
	"github.com/samber/oops"
)

// Contribute registers connection.signIn, a live call (never a tool: a
// sign-in is a person's).
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Calls.Register(reg, live.Call{
		Descriptor: extensions.Descriptor{ID: "connection.signIn", Source: "connectors"}, Title: "Sign a connection in",
		Description: "Starts an OAuth sign-in for a connection whose type has one: args {id, return?}; ends with one result event {url}, the provider's page to open in a popup. The provider sends the person back to the callback, which keeps the tokens on the connection.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			var a struct{ ID, Return string }
			if err := json.Unmarshal(c.Args, &a); err != nil || a.ID == "" {
				return c.Fail(http.StatusBadRequest, log.Code("invalid_args").Errorf("connection.signIn needs an id"))
			}
			u, err := Start(c.ServiceArgs, deps(), a.ID, a.Return)
			if err != nil {
				return c.Fail(statusOf(err), err)
			}
			c.Emit("result", map[string]any{"url": u})
			return nil
		},
	})
}

// statusOf is the HTTP status of a sign-in error.
func statusOf(err error) uint16 {
	code := ""
	if o, ok := oops.AsOops(err); ok {
		code = fmt.Sprint(o.Code())
	}
	switch code {
	case "connection_not_found":
		return http.StatusNotFound
	case "forbidden", "oauth_state_other", "oauth_person_only":
		return http.StatusForbidden
	case "oauth_not_supported", "oauth_not_registered", "oauth_no_redirect", "oauth_state_unknown", "oauth_denied", "oauth_provider_changed", "invalid_args":
		return http.StatusBadRequest
	case "oauth_exchange_failed", "oauth_account":
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

// Register serves the callback under router (the /api/v1 group of signed-in
// users), at /connectors/oauth/callback.
func Register(router gin.IRouter, deps func() Deps) {
	router.GET("/connectors/oauth/callback", func(c *gin.Context) {
		args := rest.Args(c)
		done, err := Finish(args, deps(), c.Query("state"), c.Query("code"), c.Query("error"))
		page := result{Connection: done.Connection, Account: done.Account, Return: done.Return}
		status := http.StatusOK
		if err != nil {
			resp := args.Fail(statusOf(err), err) // logged once here
			page.Error, page.Message, status = resp.ErrorCode, resp.Message, int(resp.Code)
		}
		if page.Return == "" {
			page.Return = "/workbench"
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		c.Status(status)
		_ = callbackPage.Execute(c.Writer, page)
	})
}

// result is what the popup tells its opener.
type result struct {
	Connection string `json:"connection"`
	Account    string `json:"account,omitempty"`
	Error      string `json:"error,omitempty"`
	Message    string `json:"message,omitempty"`
	Return     string `json:"-"`
}

// callbackPage tells the window that opened the popup how the sign-in
// went and closes; opened without a popup (blocked), it goes back to the
// route the sign-in started from.
var callbackPage = template.Must(template.New("callback").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Signing in</title>
<style>body{font-family:system-ui,sans-serif;background:#121212;color:#eee;display:grid;place-items:center;height:100vh;margin:0}</style>
</head><body><p>{{if .Error}}The sign-in failed: {{.Message}}{{else}}Signed in{{if .Account}} as {{.Account}}{{end}}. You can close this window.{{end}}</p>
<script>
(function () {
  var msg = {type: "connector.oauth", connection: {{.Connection}}, account: {{.Account}}, error: {{.Error}}, message: {{.Message}}};
  if (window.opener) {
    window.opener.postMessage(msg, window.location.origin);
    window.close();
  } else {
    var back = {{.Return}};
    window.location.replace(back + (back.indexOf("?") < 0 ? "?" : "&") + "oauth=" + encodeURIComponent(msg.error || "done"));
  }
})();
</script></body></html>`))
