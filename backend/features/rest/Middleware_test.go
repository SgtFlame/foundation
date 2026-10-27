package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/gin-gonic/gin"
)

func router(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware())
	r.POST("/x", handler)
	return r
}

func TestTraceAndPrincipal(t *testing.T) {
	var args features.ServiceArgs
	r := router(func(c *gin.Context) {
		args = Args(c)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set(TraceHeader, "browser-trace-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get(TraceHeader); got != "browser-trace-1" {
		t.Errorf("echoed trace = %q", got)
	}
	if logging.TraceFrom(args.Ctx) != "browser-trace-1" {
		t.Errorf("context trace = %q", logging.TraceFrom(args.Ctx))
	}
	if args.Principal != features.LocalPrincipal() {
		t.Errorf("principal = %+v", args.Principal)
	}

	// A missing or unsafe header gets a new trace
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set(TraceHeader, "bad trace\n")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get(TraceHeader); len(got) != 32 {
		t.Errorf("new trace = %q", got)
	}
}

func TestBindFailure(t *testing.T) {
	r := router(func(c *gin.Context) {
		var req struct{ N int }
		if _, ok := Bind(c, &req); ok {
			c.Status(http.StatusNoContent)
		}
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"N":"x"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["errorCode"] != "bad_request" || body["message"] == "" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["trace"]; ok {
		t.Error("the trace must not be in the body")
	}
}
