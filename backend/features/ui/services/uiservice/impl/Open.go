package impl

import (
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
)

// Open opens a page as a session, resuming the stored view state of the
// same session id when it is the same user's and page's; a session open on
// another connection (one that dropped) moves to this one.
func (s *service) Open(args uiservice.OpenArgs) uiservice.OpenResponse {
	fail := func(f *features.ServiceResponse) uiservice.OpenResponse { return uiservice.OpenResponse{Common: *f} }
	var page *ui.Page
	for _, p := range ui.Pages.All(s.reg) {
		if p.ID == args.Page {
			page = &p
			break
		}
	}
	// An installed app's (forge 05d)
	for _, src := range ui.AppSources.All(s.reg) {
		if page != nil || src.Page == nil {
			break
		}
		p, mine, err := src.Page(args.ServiceArgs, args.Page)
		switch {
		case err != nil:
			return fail(args.Fail(http.StatusNotFound, log.Code("app_page_gone").Public(err.Error()).Wrap(err)))
		case mine:
			page = &p
		}
	}
	if page == nil {
		return fail(args.Fail(http.StatusNotFound, log.Code("page_not_found").Errorf("there is no page %q", args.Page)))
	}
	if !live.ValidToken(args.Session) {
		return fail(args.Fail(http.StatusBadRequest, log.Code("invalid_session").Errorf("%q is not a session id", args.Session)))
	}
	if err := reach(args.ServiceArgs, *page); err != nil {
		return fail(args.Fail(authz.Status, err))
	}
	if len(args.Params) == 0 {
		args.Params = json.RawMessage(`{}`)
	}

	s.mu.Lock()
	old := s.sessions[args.Session]
	s.mu.Unlock()
	if old != nil {
		if old.Principal().UserID != args.Principal.UserID {
			return fail(args.Fail(http.StatusForbidden, log.Code("session_taken").New("this session is someone else's")))
		}
		old.Close(false)
	}
	var rec *ui.Record
	if s.store != nil {
		r, err := s.store.Load(args.Context(), args.Principal.TenantID, args.Session)
		if err != nil {
			log.Error(args.Context(), err, "cannot read UI session %s; opening afresh", args.Session)
		}
		if r != nil && r.UserID != args.Principal.UserID {
			return fail(args.Fail(http.StatusForbidden, log.Code("session_taken").New("this session is someone else's")))
		}
		if r != nil && r.Page == page.ID && string(r.Params) == string(args.Params) {
			rec = r
		}
	}
	sess, f := ui.Open(args.ServiceArgs, args.Session, *page, args.Params, rec, ui.Options{
		Catalog: ui.Catalog(s.reg), Commander: commander{s.bus}, Graphs: s.graphs, Subs: s.subs, Store: s.store, Send: args.Send,
	})
	if f != nil {
		return fail(f)
	}
	s.mu.Lock()
	s.sessions[args.Session] = sess
	s.mu.Unlock()
	return uiservice.OpenResponse{Common: features.OK(), Session: sess}
}

// commander decides a handler's commands on the bus.
type commander struct{ b *bus.Bus }

func (c commander) Execute(args features.ServiceArgs, id, typ, target, request string, raw json.RawMessage) ([]live.Problem, *features.ServiceResponse) {
	if c.b == nil {
		return nil, args.Fail(http.StatusServiceUnavailable, log.New("commands are not available"))
	}
	res := c.b.Execute(bus.Request{ServiceArgs: args, ID: id, Type: typ, Target: target, RequestID: request, Args: raw})
	if res.Common.Code >= 300 {
		return res.Problems, &res.Common
	}
	return nil, nil
}

// reach checks that the principal may open the page: its Action on the
// tenant, or any one of its Reach (an installed app's).
func reach(args features.ServiceArgs, page ui.Page) error {
	tenant := authz.Tenant(args.Principal)
	if len(page.Reach) == 0 {
		if page.Action == "" {
			return nil
		}
		return authz.Check(args.Context(), args.Principal, page.Action, tenant).Err(page.Action, tenant)
	}
	var first error
	for _, r := range page.Reach {
		resource := tenant
		if r.Resource != "" {
			resource += "/" + r.Resource
		}
		err := authz.Check(args.Context(), args.Principal, r.Action, resource).Err(r.Action, resource)
		if err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
	}
	return first
}
