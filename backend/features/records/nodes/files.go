package recordsnodes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/files"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

// contributeFiles registers the file nodes and Search records (Records.md
// › Built in slice 24).
func contributeFiles(reg *extensions.Registry, deps func() data.Deps) {
	node(reg, 680, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.uploadFile"}, Title: "Upload file",
		Kinds:       []string{livenodes.KindAction, livenodes.KindConnectorOp, uinodes.KindHandler},
		External:    true, // the object store, which a sandboxed run leaves alone
		Sample:      cognition.Values{"file": map[string]any{"id": "sample", "name": "sample.txt", "size": 0, "contentType": "text/plain"}},
		Description: "Keeps a file for an entity type's file fields and gives the reference to set on a record (with Send command record.update, or Update record in the command graph it sends). The content is text, or base64 when the encoding says so; the records section's maxFileMB bounds it. Not in command graphs, which may be decided more than once.",
		Example:     "Upload file for task named notes.txt from the form's text, then Send command record.update {set: {attachment: file}}",
		Examples:    cognition.ConfigExample("records.uploadFile", `{"entity":"task","encoding":"text"}`),
		Config:      json.RawMessage(`{"type":"object","properties":{` + entityConfig + `,"encoding":{"type":"string","enum":["text","base64"],"title":"Encoding","description":"How the content input is given; text when left out"}},"required":["entity"]}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("name", "Name", "string", "The file's name, e.g. notes.txt"),
				{ID: "contentType", Name: "Content type", Type: "string", Optional: true, Description: "Its media type, e.g. text/plain; none when unconnected"},
				cognition.DataPin("content", "Content", "string", "The file's content, as the encoding says")},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("file", "File", "json", "The reference {id, name, size, contentType} a file field takes")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Entity, Encoding string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			name, _ := cognition.Get[string](in, "name")
			contentType, _ := cognition.Get[string](in, "contentType")
			content, _ := cognition.Get[string](in, "content")
			body := []byte(content)
			if c.Encoding == "base64" {
				b, err := base64.StdEncoding.DecodeString(content)
				if err != nil {
					return nil, "", fmt.Errorf("the content is not base64: %v", err)
				}
				body = b
			}
			ref, err := data.Upload(x.ServiceArgs, deps(), c.Entity, name, contentType, bytes.NewReader(body))
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"file": plain(ref)}, cognition.Then, nil
		},
	})
	node(reg, 685, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.fileUrl"}, Title: "File URL",
		Description: "The URL a record's file is served at, for a link, an image or a download. It serves the file to whoever can read the record (its row rule included) and to no one else.",
		Example:     "File URL of task's attachment for the selected row, bound to an image's src",
		Examples:    cognition.ConfigExample("records.fileUrl", `{"entity":"task","field":"attachment"}`),
		Config:      json.RawMessage(`{"type":"object","properties":{` + entityConfig + `,"field":{"type":"string","title":"Field","description":"The file field's id"}},"required":["entity","field"]}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.DataPin("id", "ID", "string", "The record's id"),
				{ID: "file", Name: "File", Type: cognition.Any, Description: "The file: its reference {id, …} as the field holds it, or its id"}},
			Out: []cognition.Pin{cognition.DataPin("url", "URL", "string", "Where it is served; empty without a file")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Entity, Field string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			id, _ := cognition.Get[string](in, "id")
			var file string
			switch f := in["file"].(type) {
			case string:
				file = f
			case map[string]any:
				file, _ = f["id"].(string)
			}
			if id == "" || file == "" {
				return cognition.Values{"url": ""}, "", nil
			}
			return cognition.Values{"url": files.URL(c.Entity, id, c.Field, file)}, "", nil
		},
	})
	node(reg, 675, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "records.search"}, Title: "Search records", Volatile: true,
		Description: "Finds records whose searchable fields match a text, best first, as records.search does: in one entity type, or every type the user may read when it is left out. lexicalSemantic leans to exact words (-5) or to meaning (5); at 0 or below only records containing the words are found. A query graph runs again, debounced, when the type's records change.",
		Example:     "Search records page for the search box's text, for a search results list",
		Config:      json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string","title":"Entity type","description":"The entity type's id; every type when left out"},"limit":{"type":"integer","title":"Limit","description":"How many; 20 when left out"},"lexicalSemantic":{"type":"integer","title":"Lexical ↔ Semantic","minimum":-5,"maximum":5}}}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("text", "Text", "string", "What to search for")},
			Out: []cognition.Pin{cognition.DataPin("records", "Records", "json", "The records found, best first, each with its _score")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var q data.SearchQuery
			if err := x.Config(&q); err != nil {
				return nil, "", err
			}
			q.Text, _ = cognition.Get[string](in, "text")
			d := deps()
			if q.Entity != "" {
				watch(x, d, q.Entity)
			} else if types, err := publishedIDs(x, d); err == nil {
				watch(x, d, types...)
			}
			res, err := data.SearchRecords(x.ServiceArgs, d, q)
			if err != nil {
				return nil, "", err
			}
			rows := plain(res.Records)
			if rows == nil {
				rows = []any{}
			}
			return cognition.Values{"records": rows}, "", nil
		},
	})
}

// publishedIDs are the tenant's entity types, for a search over all of
// them.
func publishedIDs(x *cognition.Exec, d data.Deps) ([]string, error) {
	types, err := data.PublishedTypes(x.Context(), d, x.Principal.TenantID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, t.ID)
	}
	return out, nil
}
