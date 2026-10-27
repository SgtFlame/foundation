// Package logging is the one way the backend logs and makes errors. A Builder
// collects context the way samber/oops does (In, Tags, Code, With, ...) and
// either logs a record or makes an oops error with that same context, like
// the frontend's log in src/logging:
//
//	log := logging.In("index").Tags("reindex").With("files", n)
//	log.Info(ctx, "reindex started")                // a log record
//	return log.Code("reindex_failed").Wrap(err)      // an oops error
//	log.Error(ctx, err, "cannot index %s", file)     // logs err, its oops context flattened in
//
// Records go through slog, so they carry the request's trace from ctx. Their
// fields are named as in the frontend LogRecord and in VictoriaLogs (see
// documentation/plans/Logging.md).
package logging

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/samber/oops"
)

// Builder is immutable: every method returns a new one.
type Builder struct {
	domain, code, trace, span, hint, public, user, tenant string
	tags                                                  []string
	attrs                                                 []slog.Attr
}

func In(domain string) Builder    { return Builder{}.In(domain) }
func Tags(tags ...string) Builder { return Builder{}.Tags(tags...) }
func Code(code string) Builder    { return Builder{}.Code(code) }
func With(kv ...any) Builder      { return Builder{}.With(kv...) }
func Trace(trace string) Builder  { return Builder{}.Trace(trace) }

func (b Builder) In(domain string) Builder {
	b.domain = domain
	return b
}

func (b Builder) Tags(tags ...string) Builder {
	b.tags = append(slices.Clip(b.tags), tags...)
	return b
}

func (b Builder) Code(code string) Builder {
	b.code = code
	return b
}

func (b Builder) Trace(trace string) Builder {
	b.trace = trace
	return b
}

func (b Builder) Span(span string) Builder {
	b.span = span
	return b
}

func (b Builder) Hint(hint string) Builder {
	b.hint = hint
	return b
}

// Public sets the message that is safe to show users.
func (b Builder) Public(public string) Builder {
	b.public = public
	return b
}

func (b Builder) User(id string) Builder {
	b.user = id
	return b
}

func (b Builder) Tenant(id string) Builder {
	b.tenant = id
	return b
}

// With adds attributes as key/value pairs: With("volumeId", id, "files", n).
func (b Builder) With(kv ...any) Builder {
	b.attrs = slices.Clip(b.attrs)
	for i := 0; i+1 < len(kv); i += 2 {
		b.attrs = append(b.attrs, slog.Any(fmt.Sprint(kv[i]), kv[i+1]))
	}
	return b
}

// Errors: these return an oops error carrying the builder's context, and do
// not log it.

func (b Builder) New(message string) error {
	return b.oops().New(message)
}

func (b Builder) Errorf(format string, args ...any) error {
	return b.oops().Errorf(format, args...)
}

func (b Builder) Wrap(err error) error {
	return b.oops().Wrap(err)
}

func (b Builder) Wrapf(err error, format string, args ...any) error {
	return b.oops().Wrapf(err, format, args...)
}

// oops is the equivalent oops builder; its stack traces start at our caller.
func (b Builder) oops() oops.OopsErrorBuilder {
	o := oops.CallerSkip(1).In(b.domain).Tags(b.tags...).Trace(b.trace).Span(b.span).Hint(b.hint).Public(b.public)
	if b.code != "" {
		o = o.Code(b.code)
	}
	if b.user != "" {
		o = o.User(b.user)
	}
	if b.tenant != "" {
		o = o.Tenant(b.tenant)
	}
	for _, a := range b.attrs {
		o = o.With(a.Key, a.Value.Any())
	}
	return o
}

// Logging: ctx supplies the request's context (its trace, user and tenant)
// underneath the builder's own.

func (b Builder) Debug(ctx context.Context, format string, args ...any) {
	b.Log(ctx, slog.LevelDebug, nil, format, args...)
}

func (b Builder) Info(ctx context.Context, format string, args ...any) {
	b.Log(ctx, slog.LevelInfo, nil, format, args...)
}

func (b Builder) Warn(ctx context.Context, format string, args ...any) {
	b.Log(ctx, slog.LevelWarn, nil, format, args...)
}

// Error logs err at error level.
func (b Builder) Error(ctx context.Context, err error, format string, args ...any) {
	b.Log(ctx, slog.LevelError, err, format, args...)
}

// Log writes one record at level; err, when not nil, is flattened into it.
func (b Builder) Log(ctx context.Context, level slog.Level, err error, format string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	handler := slog.Default().Handler()
	if !handler.Enabled(ctx, level) {
		return
	}
	r := slog.NewRecord(time.Now(), level, fmt.Sprintf(format, args...), 0)
	FromContext(ctx).under(b).withError(err).addTo(&r)
	_ = handler.Handle(ctx, r)
}

// under returns b layered over base: b's fields win, tags and attributes add up.
func (base Builder) under(b Builder) Builder {
	out := base
	for _, f := range []struct {
		dst *string
		src string
	}{
		{&out.domain, b.domain}, {&out.code, b.code}, {&out.trace, b.trace}, {&out.span, b.span},
		{&out.hint, b.hint}, {&out.public, b.public}, {&out.user, b.user}, {&out.tenant, b.tenant},
	} {
		if f.src != "" {
			*f.dst = f.src
		}
	}
	out.tags = slices.Clip(out.tags)
	for _, t := range b.tags {
		if !slices.Contains(out.tags, t) {
			out.tags = append(out.tags, t)
		}
	}
	out.attrs = append(slices.Clip(out.attrs), b.attrs...)
	return out
}

// withError keeps err for the record, and fills what the builder lacks from
// err's oops context.
func (b Builder) withError(err error) loggedRecord {
	rec := loggedRecord{Builder: b, err: err}
	o, ok := oops.AsOops(err)
	if !ok {
		return rec
	}
	rec.oops = &o
	user, _ := o.User()
	tenant, _ := o.Tenant()
	inner := Builder{
		domain: o.Domain(), trace: o.Trace(), span: o.Span(), hint: o.Hint(),
		public: o.Public(), user: user, tenant: tenant, tags: o.Tags(),
	}
	for k, v := range o.Context() {
		inner.attrs = append(inner.attrs, slog.Any(k, v))
	}
	slices.SortFunc(inner.attrs, func(a, b slog.Attr) int { return cmp.Compare(a.Key, b.Key) })
	rec.Builder = inner.under(b)
	return rec
}

type loggedRecord struct {
	Builder
	err  error
	oops *oops.OopsError
}

func (rec loggedRecord) addTo(r *slog.Record) {
	for _, f := range []struct{ key, value string }{
		{"domain", rec.domain}, {"code", rec.code}, {"trace", rec.trace}, {"span", rec.span},
		{"hint", rec.hint}, {"public", rec.public}, {"user.id", rec.user}, {"tenant.id", rec.tenant},
	} {
		if f.value != "" {
			r.AddAttrs(slog.String(f.key, f.value))
		}
	}
	if len(rec.tags) > 0 {
		r.AddAttrs(slog.Any("tags", rec.tags))
	}
	for _, a := range rec.attrs {
		r.AddAttrs(slog.Attr{Key: "attr." + a.Key, Value: a.Value})
	}
	if rec.err == nil {
		return
	}
	r.AddAttrs(slog.String("error", rec.err.Error()))
	if rec.oops == nil {
		return
	}
	if code := rec.oops.Code(); code != nil {
		r.AddAttrs(slog.String("error.code", fmt.Sprint(code)))
	}
	if domain := rec.oops.Domain(); domain != "" {
		r.AddAttrs(slog.String("error.domain", domain))
	}
	if stack := rec.oops.Stacktrace(); stack != "" {
		r.AddAttrs(slog.String("error.stack", stack))
	}
	if cause := causeOf(rec.oops); cause != nil {
		r.AddAttrs(slog.String("error.cause", cause.Error()))
	}
}

// causeOf is the innermost error that is not an oops error, if any.
func causeOf(o *oops.OopsError) error {
	var cause error
	for err := o.Unwrap(); err != nil; {
		inner, ok := oops.AsOops(err)
		if !ok {
			cause = err
			break
		}
		err = inner.Unwrap()
	}
	return cause
}
