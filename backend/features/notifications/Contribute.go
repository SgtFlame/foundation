package notifications

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

const source = "notifications"

// Queries and the call.
const (
	MineQuery  = "notifications.mine"
	CountQuery = "notifications.count"
	ReadAll    = "notifications.readAll"
)

// ContributeAccess declares the notification resource type: apps send
// notifications in their tenant with no grant, people with create.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "notification", Source: source, Order: 96}, Title: "Notifications", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "create", Title: "Send people notifications", Class: authz.ClassCreate, Apps: true},
		},
	})
}

// userOf is the principal's user id as notifications hold it.
func userOf(p features.Principal) string {
	if p.Kind != features.PrincipalUser || p.UserID == 0 {
		return ""
	}
	return strconv.FormatUint(p.UserID, 10)
}

// feed is another query's feed with the params replaced: notifications'
// queries are records.find and records.aggregate over the caller's own.
type feed struct {
	live.Feed
	params json.RawMessage
}

func (f feed) Params() json.RawMessage { return f.params }

// delegate runs a registered query with params made from the feed's.
func delegate(reg *extensions.Registry, deps func() Deps, query string, params func(user string, in json.RawMessage) (any, error)) func(live.Feed) error {
	return func(f live.Feed) error {
		args := f.Args()
		user := userOf(args.Principal)
		if user == "" {
			return log.Code("not_a_user").New("only people have notifications")
		}
		if err := Ensure(f.Context(), deps(), args.Principal.TenantID); err != nil {
			return err
		}
		var q live.Query
		for _, c := range live.Queries.All(reg) {
			if c.ID == query {
				q = c
			}
		}
		if q.Run == nil {
			return log.Errorf("there is no query %s", query)
		}
		p, err := params(user, f.Params())
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(p)
		return q.Run(feed{Feed: f, params: raw})
	}
}

// Contribute registers the Notify node, the queries and the call.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: MineQuery, Source: source}, Title: "My notifications",
		Description: "The caller's own notifications, newest first, as records.find gives them ({records, next, plan}, changing by insert, update, move and remove): each with recipient, app, kind, title, body, link (a workbench: link), key, read and created. unread keeps only those not read; limit is the page's size (50 when left out).",
		Params:      json.RawMessage(`{"type":"object","properties":{"unread":{"type":"boolean","description":"Only those not read yet"},"limit":{"type":"integer","description":"The page's size, 50 when left out"},"cursor":{"type":"string","description":"A previous result's next"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"unread":true,"limit":20}`)},
		Run: delegate(reg, deps, "records.find", func(user string, in json.RawMessage) (any, error) {
			var a struct {
				Unread bool
				Limit  int
				Cursor string
			}
			_ = json.Unmarshal(in, &a)
			if a.Limit <= 0 {
				a.Limit = 50
			}
			where := map[string]any{"recipient": user}
			if a.Unread {
				where["read"] = false
			}
			return map[string]any{"entity": Entity, "where": where, "sort": []any{map[string]any{"field": "created", "desc": true}}, "limit": a.Limit, "cursor": a.Cursor}, nil
		}),
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: CountQuery, Source: source}, Title: "How many notifications I have not read",
		Description: "The number of the caller's unread notifications, as records.aggregate gives it: {groups: [{key: null, values: {count}}]} (no groups when there are none), kept current.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Run: delegate(reg, deps, "records.aggregate", func(user string, _ json.RawMessage) (any, error) {
			return map[string]any{"entity": Entity, "where": map[string]any{"recipient": user, "read": false}, "aggregates": []any{map[string]any{"op": "count"}}}, nil
		}),
	})

	live.Calls.Register(reg, live.Call{
		Descriptor: extensions.Descriptor{ID: ReadAll, Source: source}, Title: "Mark all my notifications read",
		Description: "Marks every unread notification of the caller read, as the caller; ends with a done event giving {count}.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			n, fail := MarkAllRead(c.ServiceArgs, deps())
			if fail != nil {
				return fail
			}
			c.Emit("done", map[string]any{"count": n})
			return nil
		},
	})

	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "notify.send", Source: source, Order: 600}, Title: "Notify", Category: "Commands", Effects: true,
		Kinds:       []string{"server.action", "app.trigger", "ui.handler", "function"},
		Description: "Sends people a notification they see in the workbench's notification centre: one per recipient, made once per key, so a retried run notifies once. Use a key naming the app, the subject and the kind (nexus:<message>:mention).",
		Example:     "Notify the mentioned users \"Ana mentioned you in #design\" with link workbench:app:nexus:channel/<id> and key nexus:<message>:mention",
		Examples:    cognition.ConfigExample("notify.send", `{"kind":"mention"}`),
		Config:      json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","title":"Kind","description":"What it is, such as mention, review or question"},"app":{"type":"string","title":"App","description":"The app it is from; the graph's own app when empty"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				{ID: "recipients", Name: "Recipients", Type: cognition.Any, Description: "A user id, or a list of them"},
				cognition.DataPin("title", "Title", "string", "One line saying what happened"),
				{ID: "body", Name: "Body", Type: "string", Optional: true, Description: "A short excerpt, plain text"},
				{ID: "link", Name: "Link", Type: "string", Optional: true, Description: "A workbench: link to what it is about"},
				{ID: "key", Name: "Key", Type: "string", Optional: true, Description: "Makes it unique per recipient: the same key notifies once"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("ids", "Ids", "json", "The notifications' record ids, one per recipient")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Kind, App string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			n := Notification{Kind: c.Kind, App: c.App, Recipients: recipients(in["recipients"])}
			n.Title, _ = cognition.Get[string](in, "title")
			n.Body, _ = in["body"].(string)
			n.Link, _ = in["link"].(string)
			n.Key, _ = in["key"].(string)
			if n.App == "" {
				if app, ok := cutApp(x.Owner); ok {
					n.App = app
				}
			}
			if bus.DryOf(x.Context()) != nil {
				return cognition.Values{"ids": []string{}}, cognition.Then, nil // a sandboxed run notifies no one
			}
			ids, fail := Notify(x.ServiceArgs, deps(), n)
			if fail != nil {
				return nil, "", fmt.Errorf("%s", fail.Message)
			}
			return cognition.Values{"ids": ids}, cognition.Then, nil
		},
	})
}

func cutApp(owner string) (string, bool) {
	if len(owner) > 4 && owner[:4] == "app:" {
		return owner[4:], true
	}
	return "", false
}

// recipients reads a user id or a list of them.
func recipients(v any) []string {
	switch r := v.(type) {
	case string:
		return []string{r}
	case float64:
		return []string{strconv.FormatFloat(r, 'f', -1, 64)}
	case json.Number:
		return []string{r.String()}
	case []string:
		return r
	case []any:
		var out []string
		for _, x := range r {
			out = append(out, recipients(x)...)
		}
		return out
	}
	return nil
}

// MarkAllRead marks every unread notification of args' user read, as the
// user.
func MarkAllRead(args features.ServiceArgs, d Deps) (int, *features.ServiceResponse) {
	user := userOf(args.Principal)
	if user == "" {
		return 0, args.Fail(http.StatusForbidden, log.Code("not_a_user").New("only people have notifications"))
	}
	if d.Projections == nil || d.Bus == nil {
		return 0, args.Fail(http.StatusServiceUnavailable, log.New("there are no records"))
	}
	where, _ := json.Marshal(user)
	res, err := data.Find(args, d, data.Query{Entity: Entity, Where: map[string]json.RawMessage{"recipient": where, "read": json.RawMessage("false")}})
	if err != nil {
		return 0, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	n := 0
	for _, r := range res.Records {
		var id string
		_ = json.Unmarshal(r[data.MetaID], &id)
		raw, _ := json.Marshal(map[string]any{"entity": Entity, "set": map[string]any{"read": true}})
		out := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: "notification-read-" + id + "-" + strconv.FormatUint(features.NewTSID(), 36), Type: "record.update", Target: id, Args: raw})
		if out.Common.Code < 300 {
			n++
		}
	}
	return n, nil
}
