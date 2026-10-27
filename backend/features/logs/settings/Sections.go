// Package logssettings declares the logging settings section
// (documentation/Logging.md › Configuration) and the Go type it
// decodes into with config.Read.
package logssettings

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// ID is the section's ID.
const ID = "logging"

// Logging is the logging section.
type Logging struct {
	VictoriaLogs struct {
		Enabled  bool   `json:"enabled"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		TLS      bool   `json:"tls"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"victoriaLogs"`
	StoredLevel  string `json:"storedLevel"`
	BrowserLevel string `json:"browserLevel"`
}

// Target is where the section stores logs: the zero Target when it is off.
func (l Logging) Target() logging.Target {
	vl := l.VictoriaLogs
	if !vl.Enabled || vl.Host == "" {
		return logging.Target{}
	}
	scheme := "http"
	if vl.TLS {
		scheme = "https"
	}
	return logging.Target{URL: fmt.Sprintf("%s://%s:%d", scheme, vl.Host, vl.Port), Username: vl.Username, Password: vl.Password}
}

// Stored is the lowest level stored, info when unset.
func (l Logging) Stored() slog.Level { return levelOr(l.StoredLevel, slog.LevelInfo) }

// Browser is the lowest level browsers forward, warn when unset.
func (l Logging) Browser() slog.Level { return levelOr(l.BrowserLevel, slog.LevelWarn) }

func levelOr(s string, fallback slog.Level) slog.Level {
	if level, err := logging.ParseLevel(s); err == nil && s != "" {
		return level
	}
	return fallback
}

// Contribute registers the logging section on config.Sections.
func Contribute(reg *extensions.Registry) {
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 900, Source: "logs"},
		Title:       "Logging",
		Description: "Where server and browser logs are stored for the Log Explorer. Logs always go to the server's stderr as well. --vlogs-url or $FOUNDATION_VLOGS_URL wins over these.",
		Category:    []string{"System"},
		Scopes:      []string{"app"},
		Schema:      json.RawMessage(schema),
		Defaults:    json.RawMessage(defaults),
		Version:     1,
		Check:       check,
	})
}

// check requires a host when storage is on.
func check(value map[string]any) []config.FieldError {
	vl, _ := value["victoriaLogs"].(map[string]any)
	if host, _ := vl["host"].(string); vl["enabled"] == true && host == "" {
		return []config.FieldError{{Path: "victoriaLogs.host", Message: "needs a host to store logs"}}
	}
	return nil
}

const levels = `[{"value": "debug", "label": "Debug"}, {"value": "info", "label": "Info"}, {"value": "warn", "label": "Warning"}, {"value": "error", "label": "Error"}]`

const schema = `{
  "type": "object",
  "properties": {
    "victoriaLogs": {
      "type": "object",
      "title": "VictoriaLogs",
      "description": "The VictoriaLogs instance logs are stored in and searched from. backend/scripts/victorialogs.sh runs one for development.",
      "properties": {
        "enabled": {"type": "boolean", "title": "Store logs in VictoriaLogs"},
        "host": {"type": "string", "title": "Host"},
        "port": {"type": "integer", "title": "Port", "min": 1, "max": 65535},
        "tls": {"type": "boolean", "title": "Use HTTPS"},
        "username": {"type": "string", "title": "Username", "description": "For an instance behind an auth proxy"},
        "password": {"type": "secret", "title": "Password"}
      }
    },
    "storedLevel": {"type": "enum", "title": "Lowest level stored", "options": ` + levels + `},
    "browserLevel": {"type": "enum", "title": "Lowest level forwarded from browsers", "options": ` + levels + `}
  }
}`

const defaults = `{
  "victoriaLogs": {"enabled": false, "host": "localhost", "port": 9428, "tls": false, "username": ""},
  "storedLevel": "info",
  "browserLevel": "warn"
}`
