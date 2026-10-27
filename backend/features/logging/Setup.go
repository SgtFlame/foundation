package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/samber/oops"
)

// Setup makes slog's default logger, which every Builder writes through,
// write to w: as text ("text") or as JSON lines ("json") with VictoriaLogs'
// field names, from level up. Records also go to each sink (such as
// Victoria's Handler) that is enabled for them, so a sink never loses the
// local output.
func Setup(w io.Writer, format string, level slog.Level, sinks ...slog.Handler) error {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	case "json":
		opts.ReplaceAttr = victoriaNames
		handler = slog.NewJSONHandler(w, opts)
	default:
		return fmt.Errorf("logging: unknown format %q (text or json)", format)
	}
	if len(sinks) > 0 {
		handler = fanout(append([]slog.Handler{handler}, sinks...))
	}
	slog.SetDefault(slog.New(handler))
	// Every request has a trace, so oops need not make one per error
	oops.AutoTraceID = false
	return nil
}

// ParseLevel reads debug, info, warn or error.
func ParseLevel(s string) (slog.Level, error) {
	var level slog.Level
	err := level.UnmarshalText([]byte(s))
	return level, err
}

// victoriaNames renames slog's built-in fields to VictoriaLogs' reserved
// _time and _msg, and lower-cases the level, as the frontend's records are.
func victoriaNames(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		a.Key = "_time"
	case slog.MessageKey:
		a.Key = "_msg"
	case slog.LevelKey:
		a.Value = slog.StringValue(strings.ToLower(a.Value.String()))
	}
	return a
}

// fanout hands each record to every handler enabled for its level.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range f {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}
