package data

import (
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// ErrTooLarge is a file over the records section's maxFileMB.
var ErrTooLarge = errors.New("the file is larger than the records section's maxFileMB")

// Upload keeps a file for an entity type's file fields, as the principal:
// whoever may create or change its records may upload. A record command
// then sets the reference it gives; it takes only a file its sender
// uploaded.
func Upload(args features.ServiceArgs, d Deps, entity, name, contentType string, content io.Reader) (records.FileRef, error) {
	if !live.ValidToken(entity) {
		return records.FileRef{}, log.Code("bad_entity").Errorf("%q is not an entity type id", entity)
	}
	resource := records.Resource(args.Principal, entity)
	if !authz.Check(args.Context(), args.Principal, "record.create", resource).Allowed {
		if err := authz.Check(args.Context(), args.Principal, "record.update", resource).Err("record.update", resource); err != nil {
			return records.FileRef{}, err
		}
	}
	if d.Files == nil {
		return records.FileRef{}, log.Code("files_unavailable").Errorf("files are not kept here")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\") {
		return records.FileRef{}, log.Code("bad_file_name").Errorf("%q is not a file name", name)
	}
	max := int64(d.settings(args).MaxFileMB) << 20
	ref, err := d.Files.Put(args.Context(), args.Principal.TenantID, args.Principal.UserID, name, contentType, content, max)
	if errors.Is(err, ErrTooLarge) {
		return ref, log.Code("file_too_large").Errorf("%s is larger than %d MB", name, max>>20)
	}
	return ref, err
}

// HeldBy names the record a file reference is held by, so that whoever may
// read that record may use the file (forge 07n question 3): a connector
// call's argument {id, …, heldBy: {entity, record, field}}.
type HeldBy struct {
	Entity string `json:"entity"`
	Record string `json:"record"`
	Field  string `json:"field"`
}

// HeldFiles are the ids of the file references in a connector call's args
// that name a record holding them (heldBy) which the principal may read:
// the record exists, the principal holds record.read on its type and its
// row rule shows it, and the field holds the file. The connector
// dispatcher vouches for them, so an adapter lets the caller use a file it
// did not upload, such as a draft's attachment sent by the outbox (forge
// 07n). A reference whose holder does not check out is left out, not
// refused: the adapter then refuses the file as it would anyway.
func HeldFiles(deps Deps) func(args features.ServiceArgs, callArgs json.RawMessage) ([]string, error) {
	return func(args features.ServiceArgs, callArgs json.RawMessage) ([]string, error) {
		var v any
		if len(callArgs) == 0 || json.Unmarshal(callArgs, &v) != nil {
			return nil, nil
		}
		type ref struct {
			id string
			by HeldBy
		}
		var refs []ref
		var walk func(x any)
		walk = func(x any) {
			switch t := x.(type) {
			case map[string]any:
				if id, _ := t["id"].(string); id != "" {
					if hb, ok := t["heldBy"].(map[string]any); ok {
						b, _ := json.Marshal(hb)
						var h HeldBy
						if json.Unmarshal(b, &h) == nil && h.Entity != "" && h.Record != "" && h.Field != "" {
							refs = append(refs, ref{id: id, by: h})
						}
					}
				}
				for _, y := range t {
					walk(y)
				}
			case []any:
				for _, y := range t {
					walk(y)
				}
			}
		}
		walk(v)
		var out []string
		seen := map[string]bool{}
		for _, r := range refs {
			if seen[r.id] {
				continue
			}
			resource := records.Resource(args.Principal, r.by.Entity)
			if !live.ValidToken(r.by.Entity) || !authz.Check(args.Context(), args.Principal, "record.read", resource).Allowed {
				continue
			}
			rec, _, err := Get(args, deps, r.by.Entity, r.by.Record)
			if err != nil || rec == nil {
				continue
			}
			e, problems := Entity(args, deps.Projections, r.by.Entity)
			if problems != nil {
				continue
			}
			f, _, ok := e.FieldByID(r.by.Field)
			if !ok || f.Hidden {
				continue
			}
			if slices.ContainsFunc(records.FilesOf(f, rec[f.ID]), func(x records.FileRef) bool { return x.ID == r.id }) {
				seen[r.id] = true
				out = append(out, r.id)
			}
		}
		return out, nil
	}
}
