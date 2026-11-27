package files

import (
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// Files is what the files data service keeps (forge 21d-4): the store
// in its own process, filesservice.Remote elsewhere.
type Files interface {
	data.Files
	// Open reads a file; ok is false when there is none. The caller
	// closes the reader.
	Open(ctx context.Context, tenant uint64, id string) (io.ReadCloser, records.FileRef, bool, error)
	// Delete removes a file; one already gone is no error.
	Delete(ctx context.Context, tenant uint64, id string) error
}

var _ Files = (*Store)(nil)

// PartSize is the largest part of an upload sent in one call.
const PartSize = 256 << 10

func partName(upload string, index int) string {
	return "upload/" + upload + "/" + strconv.Itoa(index)
}

// Delete removes a file; one already gone is no error.
func (s *Store) Delete(ctx context.Context, tenant uint64, id string) error {
	b, err := s.bucket(ctx, tenant)
	if err != nil {
		return err
	}
	if err := b.Delete(ctx, id); err != nil && !errors.Is(err, jetstream.ErrObjectNotFound) {
		return log.Wrap(err)
	}
	return nil
}

// PutPart keeps one part of an upload sent in parts, under the upload's
// id, so the parts may reach any process of the service; sending a part
// again replaces it.
func (s *Store) PutPart(ctx context.Context, tenant uint64, upload string, index int, content []byte) error {
	b, err := s.bucket(ctx, tenant)
	if err != nil {
		return err
	}
	if _, err := b.PutBytes(ctx, partName(upload, index), content); err != nil {
		return log.Wrap(err)
	}
	return nil
}

// PutParts stores an upload's parts, in order, as one file (as Put), and
// removes the parts.
func (s *Store) PutParts(ctx context.Context, tenant, user uint64, upload string, parts int, name, contentType string, max int64) (records.FileRef, error) {
	b, err := s.bucket(ctx, tenant)
	if err != nil {
		return records.FileRef{}, err
	}
	defer func() {
		for i := 0; i < parts; i++ {
			_ = b.Delete(context.Background(), partName(upload, i))
		}
	}()
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < parts; i++ {
			raw, err := b.GetBytes(ctx, partName(upload, i))
			if err != nil {
				pw.CloseWithError(log.Wrapf(err, "part %d of upload %s", i, upload))
				return
			}
			if _, err := pw.Write(raw); err != nil {
				return
			}
		}
		pw.Close()
	}()
	defer pr.Close()
	return s.Put(ctx, tenant, user, name, contentType, pr, max)
}
