package impl

import (
	"errors"
	"io"
	"net/http"
	"regexp"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/files"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice"
)

var uploadID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (s *service) unavailable(args features.ServiceArgs) *features.ServiceResponse {
	if s.store == nil {
		return args.Fail(http.StatusServiceUnavailable, log.Code("files_unavailable").New("files are not kept here"))
	}
	return nil
}

func badUpload(args features.ServiceArgs, upload string) *features.ServiceResponse {
	if !uploadID.MatchString(upload) {
		return args.Fail(http.StatusBadRequest, log.Code("bad_upload").Errorf("%q is not an upload id", upload))
	}
	return nil
}

func (s *service) PutPart(args filesservice.PutPartArgs) filesservice.PutPartResponse {
	if f := s.unavailable(args.ServiceArgs); f != nil {
		return filesservice.PutPartResponse{Common: *f}
	}
	if f := badUpload(args.ServiceArgs, args.Upload); f != nil {
		return filesservice.PutPartResponse{Common: *f}
	}
	if args.Index < 0 || len(args.Data) > files.PartSize {
		return filesservice.PutPartResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_part").Errorf("a part is at most %d bytes, at an index from 0", files.PartSize))}
	}
	if err := s.store.PutPart(args.Context(), args.Principal.TenantID, args.Upload, args.Index, args.Data); err != nil {
		return filesservice.PutPartResponse{Common: *args.Fail(http.StatusInternalServerError, err)}
	}
	return filesservice.PutPartResponse{Common: features.OK()}
}

func (s *service) Put(args filesservice.PutArgs) filesservice.PutResponse {
	if f := s.unavailable(args.ServiceArgs); f != nil {
		return filesservice.PutResponse{Common: *f}
	}
	if f := badUpload(args.ServiceArgs, args.Upload); f != nil {
		return filesservice.PutResponse{Common: *f}
	}
	ref, err := s.store.PutParts(args.Context(), args.Principal.TenantID, args.Principal.UserID, args.Upload, args.Parts, args.Name, args.ContentType, args.Max)
	if errors.Is(err, data.ErrTooLarge) {
		return filesservice.PutResponse{Common: *args.Fail(http.StatusRequestEntityTooLarge, log.Code("file_too_large").New("the file is too large"))}
	}
	if err != nil {
		return filesservice.PutResponse{Common: *args.Fail(http.StatusInternalServerError, err)}
	}
	return filesservice.PutResponse{Common: features.OK(), File: ref}
}

func (s *service) Uploader(args filesservice.UploaderArgs) filesservice.UploaderResponse {
	if f := s.unavailable(args.ServiceArgs); f != nil {
		return filesservice.UploaderResponse{Common: *f}
	}
	user, ok, err := s.store.Uploader(args.Context(), args.Principal.TenantID, args.ID)
	if err != nil {
		return filesservice.UploaderResponse{Common: *args.Fail(http.StatusInternalServerError, err)}
	}
	return filesservice.UploaderResponse{Common: features.OK(), User: user, Found: ok}
}

func (s *service) Delete(args filesservice.DeleteArgs) filesservice.DeleteResponse {
	if f := s.unavailable(args.ServiceArgs); f != nil {
		return filesservice.DeleteResponse{Common: *f}
	}
	if err := s.store.Delete(args.Context(), args.Principal.TenantID, args.ID); err != nil {
		return filesservice.DeleteResponse{Common: *args.Fail(http.StatusInternalServerError, err)}
	}
	return filesservice.DeleteResponse{Common: features.OK()}
}

// Get sends the file's reference, then its content in parts of at most
// files.PartSize, until the read ends.
func (s *service) Get(args filesservice.GetArgs) *filesservice.GetRequest {
	parts := make(chan filesservice.GetPart, 4)
	ctx := args.Context()
	send := func(p filesservice.GetPart) bool {
		select {
		case parts <- p:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer close(parts)
		if f := s.unavailable(args.ServiceArgs); f != nil {
			send(filesservice.GetPart{Failed: f})
			return
		}
		r, ref, ok, err := s.store.Open(ctx, args.Principal.TenantID, args.ID)
		if err != nil {
			send(filesservice.GetPart{Failed: args.Fail(http.StatusInternalServerError, err)})
			return
		}
		if !ok {
			send(filesservice.GetPart{Missing: true})
			return
		}
		defer r.Close()
		if !send(filesservice.GetPart{File: &ref}) {
			return
		}
		buf := make([]byte, files.PartSize)
		for {
			n, err := io.ReadFull(r, buf)
			if n > 0 && !send(filesservice.GetPart{Data: append([]byte(nil), buf[:n]...)}) {
				return
			}
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return
			}
			if err != nil {
				send(filesservice.GetPart{Failed: args.Fail(http.StatusInternalServerError, log.Wrap(err))})
				return
			}
		}
	}()
	return &filesservice.GetRequest{Parts: parts}
}
