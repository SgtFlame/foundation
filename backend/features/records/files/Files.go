// Package files keeps the content of records' file fields (Records.md ›
// Built in slice 24): a JetStream object store bucket per tenant,
// FILES_<tenant>, each object named by a new TSID with its name, content
// type and uploader in its metadata. A file field holds a reference
// (records.FileRef) to one; the record commands accept only a file its
// setter uploaded (data.Files), and recordservice serves a file only to
// someone who can read a record holding it.
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

var log = records.Log.Tags("files")

// Metadata keys of an uploaded object.
const (
	metaName        = "name"
	metaContentType = "content-type"
	metaUploadedBy  = "uploaded-by"
)

// Store is the tenants' file buckets.
type Store struct {
	js jetstream.JetStream

	mu      sync.Mutex
	buckets map[uint64]jetstream.ObjectStore
}

// New keeps files in js's object stores.
func New(js jetstream.JetStream) *Store {
	return &Store{js: js, buckets: map[uint64]jetstream.ObjectStore{}}
}

// Bucket is a tenant's bucket name.
func Bucket(tenant uint64) string { return fmt.Sprintf("FILES_%d", tenant) }

func (s *Store) bucket(ctx context.Context, tenant uint64) (jetstream.ObjectStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.buckets[tenant]; ok {
		return b, nil
	}
	b, err := s.js.CreateOrUpdateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket: Bucket(tenant), Description: "Files of records' file fields", Storage: jetstream.FileStorage,
	})
	if err != nil {
		return nil, log.Wrapf(err, "cannot open the file bucket of tenant %d", tenant)
	}
	s.buckets[tenant] = b
	return b, nil
}

// Put stores a file a user uploads and returns its reference. max is the
// largest size taken; a larger file is refused with data.ErrTooLarge and not
// kept.
func (s *Store) Put(ctx context.Context, tenant, user uint64, name, contentType string, r io.Reader, max int64) (records.FileRef, error) {
	b, err := s.bucket(ctx, tenant)
	if err != nil {
		return records.FileRef{}, err
	}
	id := strconv.FormatUint(features.NewTSID(), 10)
	limited := &limitReader{r: r, left: max}
	info, err := b.Put(ctx, jetstream.ObjectMeta{Name: id, Metadata: map[string]string{
		metaName: name, metaContentType: contentType, metaUploadedBy: strconv.FormatUint(user, 10),
	}}, limited)
	if limited.over {
		if info != nil || err == nil {
			_ = b.Delete(context.Background(), id)
		}
		return records.FileRef{}, data.ErrTooLarge
	}
	if err != nil {
		return records.FileRef{}, log.Wrap(err)
	}
	return records.FileRef{ID: id, Name: name, Size: int64(info.Size), ContentType: contentType}, nil
}

// Uploader is who uploaded a file; ok is false when there is none.
func (s *Store) Uploader(ctx context.Context, tenant uint64, id string) (uint64, bool, error) {
	b, err := s.bucket(ctx, tenant)
	if err != nil {
		return 0, false, err
	}
	info, err := b.GetInfo(ctx, id)
	if errors.Is(err, jetstream.ErrObjectNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, log.Wrap(err)
	}
	user, _ := strconv.ParseUint(info.Metadata[metaUploadedBy], 10, 64)
	return user, true, nil
}

// Open reads a file; ok is false when there is none. The caller closes
// the reader.
func (s *Store) Open(ctx context.Context, tenant uint64, id string) (io.ReadCloser, records.FileRef, bool, error) {
	b, err := s.bucket(ctx, tenant)
	if err != nil {
		return nil, records.FileRef{}, false, err
	}
	res, err := b.Get(ctx, id)
	if errors.Is(err, jetstream.ErrObjectNotFound) {
		return nil, records.FileRef{}, false, nil
	}
	if err != nil {
		return nil, records.FileRef{}, false, log.Wrap(err)
	}
	info, err := res.Info()
	if err != nil {
		res.Close()
		return nil, records.FileRef{}, false, log.Wrap(err)
	}
	return res, records.FileRef{ID: id, Name: info.Metadata[metaName], Size: int64(info.Size), ContentType: info.Metadata[metaContentType]}, true, nil
}

// limitReader reads at most left bytes and notes whether there were more.
type limitReader struct {
	r    io.Reader
	left int64
	over bool
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		var one [1]byte
		if n, _ := l.r.Read(one[:]); n > 0 {
			l.over = true
			return 0, data.ErrTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

// URL is where a record field's file is served, under /api/v1.
func URL(entity, record, field, id string) string {
	return "/api/v1/records/files/" + entity + "/" + record + "/" + field + "/" + id
}
