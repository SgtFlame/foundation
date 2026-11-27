package filesservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/files"
)

// Remote is files.Files through the service, for a process that holds no
// handle to the object stores (forge 21d-4).
func Remote(svc Service) files.Files { return remote{svc} }

type remote struct{ svc Service }

// argsFor is the call as the store's caller makes it: the principal its
// context carries when it is in tenant, else the tenant's service.
func argsFor(ctx context.Context, tenant uint64) features.ServiceArgs {
	if p, _, ok := features.CallFrom(ctx); ok && p.TenantID == tenant {
		return features.ServiceArgs{Ctx: ctx, Principal: p}
	}
	return features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipalFor(tenant, "files")}
}

type failure struct{ r features.ServiceResponse }

func (f failure) Error() string { return f.r.Message }

func failed(r features.ServiceResponse) error {
	if r.Code >= 200 && r.Code < 300 {
		return nil
	}
	if r.ErrorCode == "file_too_large" {
		return data.ErrTooLarge
	}
	return failure{r}
}

func (x remote) Put(ctx context.Context, tenant, user uint64, name, contentType string, r io.Reader, max int64) (records.FileRef, error) {
	args := argsFor(ctx, tenant)
	if user != 0 {
		args.Principal = features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: tenant}
	}
	upload := strconv.FormatUint(features.NewTSID(), 10)
	buf := make([]byte, files.PartSize)
	parts := 0
	var size int64
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			size += int64(n)
			if size > max {
				return records.FileRef{}, data.ErrTooLarge
			}
			if e := failed(x.svc.PutPart(PutPartArgs{ServiceArgs: args, Upload: upload, Index: parts, Data: buf[:n]}).Common); e != nil {
				return records.FileRef{}, e
			}
			parts++
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return records.FileRef{}, err
		}
	}
	res := x.svc.Put(PutArgs{ServiceArgs: args, Upload: upload, Parts: parts, Name: name, ContentType: contentType, Max: max})
	if e := failed(res.Common); e != nil {
		return records.FileRef{}, e
	}
	return res.File, nil
}

func (x remote) Uploader(ctx context.Context, tenant uint64, id string) (uint64, bool, error) {
	res := x.svc.Uploader(UploaderArgs{ServiceArgs: argsFor(ctx, tenant), ID: id})
	if e := failed(res.Common); e != nil {
		return 0, false, e
	}
	return res.User, res.Found, nil
}

func (x remote) Delete(ctx context.Context, tenant uint64, id string) error {
	return failed(x.svc.Delete(DeleteArgs{ServiceArgs: argsFor(ctx, tenant), ID: id}).Common)
}

// Open reads the file's reference at once and its content as the reader
// is read; closing the reader ends the read.
func (x remote) Open(ctx context.Context, tenant uint64, id string) (io.ReadCloser, records.FileRef, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	req := x.svc.Get(GetArgs{ServiceArgs: argsFor(ctx, tenant), ID: id})
	first, ok := <-req.Parts
	switch {
	case !ok:
		cancel()
		return nil, records.FileRef{}, false, errors.New("the file's read ended early")
	case first.Failed != nil:
		cancel()
		return nil, records.FileRef{}, false, failure{*first.Failed}
	case first.Missing || first.File == nil:
		cancel()
		return nil, records.FileRef{}, false, nil
	}
	return &partReader{parts: req.Parts, cancel: cancel}, *first.File, true, nil
}

type partReader struct {
	parts  <-chan GetPart
	cancel context.CancelFunc
	cur    bytes.Reader
	err    error
}

func (r *partReader) Read(p []byte) (int, error) {
	for r.cur.Len() == 0 {
		if r.err != nil {
			return 0, r.err
		}
		part, ok := <-r.parts
		if !ok {
			r.err = io.EOF
			continue
		}
		if part.Failed != nil {
			r.err = failure{*part.Failed}
			continue
		}
		r.cur.Reset(part.Data)
	}
	return r.cur.Read(p)
}

func (r *partReader) Close() error {
	r.cancel()
	for range r.parts {
	}
	return nil
}
