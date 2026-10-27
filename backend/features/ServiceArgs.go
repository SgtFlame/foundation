package features

import "context"

// ServiceArgs is embedded in every operation's args. Handlers fill it from
// the request with rest.Args; services pass it on to the services they call,
// so the principal and the trace follow the work.
type ServiceArgs struct {
	Ctx       context.Context // the request's: cancellation, trace and log context
	Principal Principal
	Meta      Meta // what the work is for, set by the caller that knows
}

// Context is Ctx, or the background context when none was given.
func (a ServiceArgs) Context() context.Context {
	if a.Ctx == nil {
		return context.Background()
	}
	return a.Ctx
}

// CallContext is Context carrying the principal and Meta, for code below the
// service layer that only gets a context, such as the AI provider decorators
// that record usage (CallFrom reads them back).
func (a ServiceArgs) CallContext() context.Context {
	return context.WithValue(a.Context(), callKey{}, call{a.Principal, a.Meta})
}
