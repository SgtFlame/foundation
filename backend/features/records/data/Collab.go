package data

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// A record's markdown field edited together (forge 07l, doc.markdown): the
// collaboration room's document kind "record", referenced by {entity, id,
// field}. The room seeds from the field's value as the writer reads it,
// and the writer's saves are record.update commands as the writer, so each
// save is an event that search, row rules and live queries see.
const DocumentKind = "record"

// ContributeCollab registers the record document kind.
func ContributeCollab(reg *extensions.Registry, deps func() Deps) {
	collabservice.DocumentKinds.Register(reg, collabservice.DocumentKind{
		Descriptor:  extensions.Descriptor{ID: DocumentKind, Source: "records"},
		Description: "A record's markdown field: {entity, id, field}",
		Open: func(args features.ServiceArgs, ref json.RawMessage) (collabservice.Document, *features.ServiceResponse) {
			return openFieldDoc(args, deps(), ref)
		},
	})
}

type fieldDoc struct {
	deps                Deps
	entity, id, field   string
	tenant              uint64
	entityName, fieldNm string
}

func openFieldDoc(args features.ServiceArgs, d Deps, raw json.RawMessage) (collabservice.Document, *features.ServiceResponse) {
	var ref struct{ Entity, ID, Field string }
	if err := json.Unmarshal(raw, &ref); err != nil || !live.ValidToken(ref.Entity) || !live.ValidToken(ref.ID) || ref.Field == "" {
		return nil, args.Fail(http.StatusBadRequest, log.Code("bad_document").Errorf("a record document is {entity, id, field}"))
	}
	e, problems := Entity(args, d.Projections, ref.Entity)
	if problems != nil {
		return nil, args.Fail(http.StatusNotFound, log.Code("entity_not_found").Errorf("%s", problems[0].Message))
	}
	f, _, ok := e.FieldByID(ref.Field)
	if !ok || f.Hidden {
		return nil, args.Fail(http.StatusNotFound, log.Code("field_not_found").Errorf("%s has no field %q", e.Name, ref.Field))
	}
	if f.Type != "markdown" {
		return nil, args.Fail(http.StatusBadRequest, log.Code("not_markdown").Errorf("%s is a %s field, not markdown", f.Name, f.Type))
	}
	return &fieldDoc{deps: d, entity: ref.Entity, id: ref.ID, field: ref.Field, tenant: args.Principal.TenantID, entityName: e.Name, fieldNm: f.Name}, nil
}

// Key is per tenant, so two tenants never share a room.
func (f *fieldDoc) Key() string {
	return fmt.Sprintf("record:%d:%s:%s:%s", f.tenant, f.entity, f.id, f.field)
}

func (f *fieldDoc) Name() string { return fmt.Sprintf("%s %s's %s", f.entityName, f.id, f.fieldNm) }

// Access: the record must be one the principal reads (the row rule, too);
// without record.update on it, the room is read-only for them.
func (f *fieldDoc) Access(args features.ServiceArgs) (bool, *features.ServiceResponse) {
	if args.Principal.TenantID != f.tenant {
		return false, args.Fail(http.StatusNotFound, log.Code("record_not_found").Errorf("there is no such record"))
	}
	resource := records.Resource(args.Principal, f.entity)
	if err := authz.Check(args.Context(), args.Principal, "record.read", resource).Err("record.read", resource); err != nil {
		return false, args.Fail(authz.Status, err)
	}
	r, _, err := Get(args, f.deps, f.entity, f.id)
	if err != nil {
		return false, args.Fail(http.StatusInternalServerError, err)
	}
	if r == nil {
		return false, args.Fail(http.StatusNotFound, log.Code("record_not_found").Errorf("there is no record %s of %s", f.id, f.entityName))
	}
	if !authz.Check(args.Context(), args.Principal, "record.update", resource).Allowed {
		return true, nil
	}
	e, problems := Entity(args, f.deps.Projections, f.entity)
	if problems != nil {
		return true, nil
	}
	return !records.Allows(first(e.RuleFor(args.Context(), args.Principal, "update")), r, args.Principal), nil
}

// Load is the field's text; none is empty.
func (f *fieldDoc) Load(args features.ServiceArgs) (string, error) {
	r, _, err := Get(args, f.deps, f.entity, f.id)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", log.Code("record_not_found").Errorf("there is no record %s of %s", f.id, f.entityName)
	}
	var s string
	if raw := r[f.field]; !records.IsNull(raw) {
		_ = json.Unmarshal(raw, &s)
	}
	return s, nil
}

// Save sets the field with record.update as the writer; text the record
// already holds commits nothing.
func (f *fieldDoc) Save(args features.ServiceArgs, text string) error {
	if f.deps.Bus == nil {
		return log.Code("bus_unavailable").Errorf("records cannot be changed here")
	}
	value, _ := json.Marshal(text)
	a, _ := json.Marshal(map[string]any{"entity": f.entity, "set": map[string]json.RawMessage{f.field: value}})
	res := f.deps.Bus.Execute(bus.Request{ServiceArgs: args, ID: "collab-" + strconv.FormatUint(features.NewTSID(), 10), Type: "record.update", Target: f.id, Args: a})
	if len(res.Problems) > 0 {
		return log.Code("save_refused").Errorf("%s", res.Problems[0].Message)
	}
	if res.Common.Code >= 300 {
		return log.Code(res.Common.ErrorCode).Errorf("%s", res.Common.Message)
	}
	return nil
}
