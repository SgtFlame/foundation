package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice"
	"github.com/SgtFlame/foundation/backend/features/text/delimited"
	"github.com/SgtFlame/foundation/backend/features/text/services/parseservice"
)

func (s *service) ParseCSV(args parseservice.ParseCSVArgs) parseservice.ParseCSVResponse {
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, parseservice.Action, resource).Err(parseservice.Action, resource); err != nil {
		return parseservice.ParseCSVResponse{Common: *args.Fail(authz.Status, err)}
	}
	content := []byte(args.Text)
	name := ""
	if args.File != "" {
		b, n, fail := s.read(args.ServiceArgs, args.File)
		if fail != nil {
			return parseservice.ParseCSVResponse{Common: *fail}
		}
		content, name = b, n
	}
	res, err := delimited.Parse(content, delimited.Options{Delimiter: args.Delimiter, Header: args.Header, Sample: args.Sample, MaxRows: args.MaxRows, MaxBytes: s.maxBytes})
	if err != nil {
		var e *delimited.Error
		if errors.As(err, &e) {
			status := uint16(http.StatusBadRequest)
			if e.Code == "file_too_large" || e.Code == "too_many_rows" {
				status = http.StatusRequestEntityTooLarge
			}
			return parseservice.ParseCSVResponse{Common: *args.Fail(status, log.Code(e.Code).New(e.Message))}
		}
		return parseservice.ParseCSVResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	cols := make([]parseservice.Column, len(res.Columns))
	for i, c := range res.Columns {
		cols[i] = parseservice.Column{Name: c.Name, Type: c.Type, Examples: c.Examples}
	}
	return parseservice.ParseCSVResponse{Common: features.OK(), Columns: cols, Rows: res.Rows, Total: res.Total,
		Delimiter: res.Delimiter, Header: res.Header, Encoding: res.Encoding, Name: name}
}

// read is an uploaded file's content and name, when the principal uploaded
// it (or is a service principal).
func (s *service) read(args features.ServiceArgs, id string) ([]byte, string, *features.ServiceResponse) {
	if s.files == nil || s.files() == nil {
		return nil, "", args.Fail(http.StatusServiceUnavailable, log.Code("files_unavailable").New("uploaded files cannot be read here"))
	}
	files := s.files()
	missing := func() ([]byte, string, *features.ServiceResponse) {
		return nil, "", args.Fail(http.StatusNotFound, log.Code("file_not_found").With("file", id).New("there is no such uploaded file"))
	}
	if args.Principal.Kind != features.PrincipalService {
		up := files.Uploader(filesservice.UploaderArgs{ServiceArgs: args, ID: id})
		if up.Common.Code >= 300 {
			return nil, "", &up.Common
		}
		// Only the uploader reads a file this way; for anyone else it is not there
		if !up.Found || up.User != args.Principal.UserID {
			return missing()
		}
	}
	req := files.Get(filesservice.GetArgs{ServiceArgs: args, ID: id})
	var out []byte
	name := ""
	for part := range req.Parts {
		switch {
		case part.Failed != nil:
			return nil, "", part.Failed
		case part.Missing:
			return missing()
		case part.File != nil:
			name = part.File.Name
			if part.File.Size > int64(s.maxBytes) {
				return nil, "", args.Fail(http.StatusRequestEntityTooLarge, log.Code("file_too_large").Errorf("the file is %d bytes, more than the %d a parse reads", part.File.Size, s.maxBytes))
			}
		default:
			out = append(out, part.Data...)
			if len(out) > s.maxBytes {
				return nil, "", args.Fail(http.StatusRequestEntityTooLarge, log.Code("file_too_large").Errorf("the file is more than the %d bytes a parse reads", s.maxBytes))
			}
		}
	}
	return out, name, nil
}
