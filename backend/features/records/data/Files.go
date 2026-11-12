package data

import (
	"errors"
	"io"
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
