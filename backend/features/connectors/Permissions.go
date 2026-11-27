package connectors

import (
	"slices"
	"sort"
)

// Permissions are the NATS subjects a user may publish and subscribe to.
type Permissions struct {
	Publish   []string `json:"publish"`
	Subscribe []string `json:"subscribe"`
}

// serviceSubjects are the wire names of the services a type may declare
// (Type.Services). Connections are resolved on cx.resolve, which every
// adapter may ask, so they add no subject.
var serviceSubjects = map[string][]string{
	"records": {"svc.record.>"},
	"files":   {"svc.files.>"},
}

// AdapterPermissions are what an external adapter serving types needs
// and all its NATS user is given (forge 21e-2): their calls in their
// queue groups, their callbacks (26b-1), connection changes, resolving connections and reporting
// sources' status, svc.connectors for its books, the services the types
// declare, and inboxes for replies and stream acks. Nothing on evt.>,
// $JS.>, $KV.>, $O.> or any other service, so a third party's adapter
// never reaches a tenant's streams, a database or another service.
func AdapterPermissions(types ...Type) Permissions {
	p := Permissions{
		Publish:   []string{"svc.connectors.>", "cx.resolve.>", "cx.status.>", "_INBOX.>"},
		Subscribe: []string{ChangedSubjects, "_INBOX.>"},
	}
	for _, t := range types {
		p.Subscribe = append(p.Subscribe, CallSubjects(t.ID))
		if t.Receives != "" { // its callbacks (forge 26b-1) and hook registrations (26b-2)
			p.Subscribe = append(p.Subscribe, ReceiveSubjects(t.ID), RegisterSubjects(t.ID))
		}
		for _, svc := range t.Services {
			p.Publish = append(p.Publish, serviceSubjects[svc]...)
		}
	}
	sort.Strings(p.Publish)
	sort.Strings(p.Subscribe)
	p.Publish, p.Subscribe = slices.Compact(p.Publish), slices.Compact(p.Subscribe)
	return p
}
