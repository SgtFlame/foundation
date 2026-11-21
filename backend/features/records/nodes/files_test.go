package recordsnodes_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
)

// files is a stand-in file store.
type files struct{ put map[string][]byte }

func (f *files) Uploader(context.Context, uint64, string) (uint64, bool, error) { return 1, true, nil }

func (f *files) Put(_ context.Context, _, _ uint64, name, contentType string, r io.Reader, _ int64) (records.FileRef, error) {
	b, _ := io.ReadAll(r)
	f.put[name] = b
	return records.FileRef{ID: "42", Name: name, Size: int64(len(b)), ContentType: contentType}, nil
}

func run(t *testing.T, types map[string]cognition.NodeType, id string, config any, in cognition.Values) cognition.Values {
	t.Helper()
	raw, _ := json.Marshal(config)
	x := &cognition.Exec{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}, Node: cognition.Node{ID: "n", Type: id, Config: raw}}
	out, _, err := types[id].Run(x, in)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return out
}

func TestFileNodes(t *testing.T) {
	reg := extensions.NewRegistry()
	store := &files{put: map[string][]byte{}}
	recordsnodes.Contribute(reg, func() data.Deps { return data.Deps{Files: store} })
	types := map[string]cognition.NodeType{}
	for _, nt := range cognition.Nodes.All(reg) {
		types[nt.ID] = nt
	}
	for _, id := range []string{"records.uploadFile", "records.fileUrl", "records.search"} {
		if problems := cognition.MetaProblems(types[id]); len(problems) > 0 {
			t.Errorf("%s lacks %v", id, problems)
		}
	}

	out := run(t, types, "records.uploadFile", map[string]any{"entity": "task", "encoding": "base64"},
		cognition.Values{"name": "a.bin", "contentType": "application/octet-stream", "content": base64.StdEncoding.EncodeToString([]byte{0, 1, 2})})
	file, _ := out["file"].(map[string]any)
	if file["id"] != "42" || file["size"] != 3.0 || string(store.put["a.bin"]) != "\x00\x01\x02" {
		t.Errorf("upload gave %v, stored %q", out, store.put["a.bin"])
	}
	out = run(t, types, "records.fileUrl", map[string]any{"entity": "task", "field": "attachment"}, cognition.Values{"id": "7", "file": file})
	if out["url"] != "/api/v1/records/files/task/7/attachment/42" {
		t.Errorf("url = %v", out["url"])
	}
	if out = run(t, types, "records.fileUrl", map[string]any{"entity": "task", "field": "attachment"}, cognition.Values{"id": "7"}); out["url"] != "" {
		t.Errorf("url without a file = %v", out["url"])
	}
}
