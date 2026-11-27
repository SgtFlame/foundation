package impl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/files"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice/proxy"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice/server"
	"github.com/SgtFlame/foundation/backend/features/services"
)

// A process without the object stores keeps and reads files through
// svc.files, in parts (forge 21d-4).
func TestFilesThroughTheProxy(t *testing.T) {
	ns := natsd.StartTest(t)
	srv, err := server.Serve(ns.Conn(), New(WithStore(files.New(ns.JS()))), services.TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	store := filesservice.Remote(proxy.New(&services.Client{NC: ns.Conn(), Caller: "test"}))
	ctx := context.Background()
	tenant := features.LocalPrincipal().TenantID

	content := bytes.Repeat([]byte("0123456789abcdef"), (2*files.PartSize+1000)/16)
	ref, err := store.Put(ctx, tenant, 7, "big.bin", "application/octet-stream", bytes.NewReader(content), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Size != int64(len(content)) || ref.Name != "big.bin" {
		t.Fatalf("ref %+v", ref)
	}
	if user, ok, err := store.Uploader(ctx, tenant, ref.ID); err != nil || !ok || user != 7 {
		t.Fatalf("uploader %d %v %v", user, ok, err)
	}
	r, got, ok, err := store.Open(ctx, tenant, ref.ID)
	if err != nil || !ok || got.ID != ref.ID || got.ContentType != "application/octet-stream" {
		t.Fatalf("open %+v %v %v", got, ok, err)
	}
	read, err := io.ReadAll(r)
	r.Close()
	if err != nil || !bytes.Equal(read, content) {
		t.Fatalf("read %d bytes of %d: %v", len(read), len(content), err)
	}
	if _, err := store.Put(ctx, tenant, 7, "big.bin", "", bytes.NewReader(content), 1000); !errors.Is(err, data.ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	if err := store.Delete(ctx, tenant, ref.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := store.Open(ctx, tenant, ref.ID); ok || err != nil {
		t.Fatalf("deleted file opened: %v %v", ok, err)
	}
	if err := store.Delete(ctx, tenant, ref.ID); err != nil {
		t.Fatalf("a second delete: %v", err)
	}
}
