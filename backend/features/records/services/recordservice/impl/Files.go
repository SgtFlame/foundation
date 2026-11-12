package impl

import (
	"net/http"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/samber/oops"
)

var log = records.Log.Tags("recordservice")

// Upload keeps a file for an entity type's file fields. Whoever may create
// or change its records may upload; the record command that sets the
// reference takes only a file its sender uploaded.
func (s *service) Upload(args recordservice.UploadArgs) recordservice.UploadResponse {
	ref, err := data.Upload(args.ServiceArgs, s.deps, args.Entity, args.Name, args.ContentType, args.Content)
	if err != nil {
		status := uint16(http.StatusInternalServerError)
		switch code := errorCode(err); {
		case code == "access_denied":
			status = authz.Status
		case code == "bad_entity" || code == "bad_file_name":
			status = http.StatusBadRequest
		case code == "file_too_large":
			status = http.StatusRequestEntityTooLarge
		case code == "files_unavailable":
			status = http.StatusServiceUnavailable
		}
		return recordservice.UploadResponse{Common: *args.Fail(status, err)}
	}
	return recordservice.UploadResponse{Common: features.OK(), File: ref}
}

// OpenFile reads a file for someone who can read a record whose field
// holds it; anyone else is told there is no such file.
func (s *service) OpenFile(args recordservice.OpenFileArgs) recordservice.OpenFileResponse {
	missing := func() recordservice.OpenFileResponse {
		return recordservice.OpenFileResponse{Common: *args.Fail(http.StatusNotFound, log.Code("file_not_found").Errorf("there is no such file"))}
	}
	resource := records.Resource(args.Principal, args.Entity)
	if err := authz.Check(args.Context(), args.Principal, "record.read", resource).Err("record.read", resource); err != nil {
		return recordservice.OpenFileResponse{Common: *args.Fail(authz.Status, err)}
	}
	if s.files == nil {
		return missing()
	}
	r, _, err := data.Get(args.ServiceArgs, s.deps, args.Entity, args.Record)
	if err != nil || r == nil {
		return missing()
	}
	e, problems := data.Entity(args.ServiceArgs, s.deps.Projections, args.Entity)
	if problems != nil {
		return missing()
	}
	f, _, ok := e.FieldByID(args.Field)
	if !ok || f.Hidden || !slices.ContainsFunc(records.FilesOf(f, r[f.ID]), func(x records.FileRef) bool { return x.ID == args.File }) {
		return missing()
	}
	content, ref, ok, err := s.files.Open(args.Context(), args.Principal.TenantID, args.File)
	if err != nil {
		return recordservice.OpenFileResponse{Common: *args.Fail(http.StatusInternalServerError, err)}
	}
	if !ok {
		return missing()
	}
	return recordservice.OpenFileResponse{Common: features.OK(), File: ref, Content: content}
}

// errorCode is an error's oops code; "" for none.
func errorCode(err error) string {
	if o, ok := oops.AsOops(err); ok {
		code, _ := o.Code().(string)
		return code
	}
	return ""
}
