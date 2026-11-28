// Package prosemirror reads ProseMirror documents, the format of the
// `document` Volume file type (forge 35a-1, documentation/MarkdownEditor.md
// › *Built in forge 35a-1*), and derives from them the plain text and the
// Markdown that the index, the model and diffs read. It is pure: no I/O.
package prosemirror

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Format is the version of a document file's envelope.
const Format = 1

// Node is one ProseMirror node, as TipTap saves it.
type Node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []Node         `json:"content,omitempty"`
	Text    string         `json:"text,omitempty"`
	Marks   []Mark         `json:"marks,omitempty"`
}

// Mark is a ProseMirror mark on a text node.
type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// File is a document file: {format: 1, doc: <ProseMirror doc>}.
type File struct {
	Format int  `json:"format"`
	Doc    Node `json:"doc"`
}

// ErrNotDocument is wrapped by every error Parse returns.
var ErrNotDocument = errors.New("not a document")

// Empty is the content of a new, empty document file.
func Empty() []byte {
	return []byte(`{"format":1,"doc":{"type":"doc","content":[{"type":"paragraph"}]}}`)
}

// Parse reads a document file. It also takes a bare ProseMirror doc
// ({type: "doc", …}), which a writer may send; Envelope turns that into a
// file.
func Parse(data []byte) (Node, error) {
	var probe struct {
		Format *int            `json:"format"`
		Doc    json.RawMessage `json:"doc"`
		Type   string          `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return Node{}, fmt.Errorf("%w: %v", ErrNotDocument, err)
	}
	raw := json.RawMessage(data)
	switch {
	case probe.Format != nil:
		if *probe.Format != Format {
			return Node{}, fmt.Errorf("%w: format %d is not %d", ErrNotDocument, *probe.Format, Format)
		}
		raw = probe.Doc
	case probe.Type == "":
		return Node{}, fmt.Errorf("%w: neither a document file nor a doc", ErrNotDocument)
	}
	var doc Node
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Node{}, fmt.Errorf("%w: %v", ErrNotDocument, err)
	}
	if doc.Type != "doc" {
		return Node{}, fmt.Errorf("%w: the root node is %q, not doc", ErrNotDocument, doc.Type)
	}
	return doc, nil
}

// Envelope returns data as a document file: unchanged when it already is
// one (so an unchanged save is byte for byte the same), wrapped when it
// is a bare doc. It fails for anything Parse refuses.
func Envelope(data []byte) ([]byte, error) {
	if _, err := Parse(data); err != nil {
		return nil, err
	}
	var probe struct {
		Format *int `json:"format"`
	}
	_ = json.Unmarshal(data, &probe)
	if probe.Format != nil {
		return data, nil
	}
	var b bytes.Buffer
	b.WriteString(`{"format":1,"doc":`)
	if err := json.Compact(&b, data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotDocument, err)
	}
	b.WriteString("}")
	return b.Bytes(), nil
}

// attr reads a string attribute.
func (n Node) attr(name string) string {
	switch v := n.Attrs[name].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprint(v)
	}
	return ""
}

// intAttr reads a whole-number attribute, else def.
func (n Node) intAttr(name string, def int) int {
	if v, ok := n.Attrs[name].(float64); ok && v == float64(int(v)) {
		return int(v)
	}
	return def
}

// ID is a block's id attribute (`id`, or `blockId`), set by the editor so
// a citation can name the block; empty when it has none.
func (n Node) ID() string {
	if id := n.attr("id"); id != "" {
		return id
	}
	return n.attr("blockId")
}
