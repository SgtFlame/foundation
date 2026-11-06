package records

import (
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Rich text (Records.md › Built in slice 24) is a ProseMirror document as
// TipTap and Milkdown save it (Forge 09-Notion question 1): {type: "doc",
// content: [nodes]}, each node {type, attrs?, content?, marks?} and text
// nodes {type: "text", text, marks?}. The records check its shape, not its
// schema, which belongs to the editor; PlainText is its text for search
// and for a model.

// maxRichTextDepth is how deeply nodes may nest.
const maxRichTextDepth = 64

// checkRichText checks a document's shape.
func checkRichText(v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok || m["type"] != "doc" {
		return nil, want(`a document {"type": "doc", "content": [...]}`, v)
	}
	if err := checkNode(m, 0, "content"); err != nil {
		return nil, err
	}
	return m, nil
}

func checkNode(n map[string]any, depth int, at string) error {
	if depth > maxRichTextDepth {
		return fmt.Errorf("%s nests more than %d deep", at, maxRichTextDepth)
	}
	t, ok := n["type"].(string)
	if !ok || t == "" {
		return fmt.Errorf("%s: a node needs a type", at)
	}
	if t == "text" {
		if _, ok := n["text"].(string); !ok {
			return fmt.Errorf("%s: a text node needs its text", at)
		}
	}
	if a, ok := n["attrs"]; ok && a != nil {
		if _, ok := a.(map[string]any); !ok {
			return fmt.Errorf("%s: attrs is an object", at)
		}
	}
	if marks, ok := n["marks"]; ok && marks != nil {
		list, ok := marks.([]any)
		if !ok {
			return fmt.Errorf("%s: marks is a list", at)
		}
		for i, mk := range list {
			mm, ok := mk.(map[string]any)
			if _, typed := mm["type"].(string); !ok || !typed {
				return fmt.Errorf("%s.marks.%d: a mark is {type, attrs?}", at, i)
			}
		}
	}
	content, ok := n["content"]
	if !ok || content == nil {
		return nil
	}
	list, ok := content.([]any)
	if !ok {
		return fmt.Errorf("%s: content is a list of nodes", at)
	}
	for i, c := range list {
		cm, ok := c.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.%d: a node is an object", at, i)
		}
		if err := checkNode(cm, depth+1, fmt.Sprintf("%s.%d", at, i)); err != nil {
			return err
		}
	}
	return nil
}

// PlainText is a rich text value's text: a line per block, with nothing
// of its marks or attributes.
func PlainText(raw json.RawMessage) string {
	var n map[string]any
	if json.Unmarshal(raw, &n) != nil {
		return ""
	}
	return strings.TrimSpace(nodeText(n))
}

func nodeText(n map[string]any) string {
	switch n["type"] {
	case "text":
		s, _ := n["text"].(string)
		return s
	case "hardBreak", "hard_break":
		return "\n"
	}
	list, _ := n["content"].([]any)
	var parts []string
	inline := false
	for _, c := range list {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		if t := cm["type"]; t == "text" || t == "hardBreak" || t == "hard_break" {
			inline = true
		}
		parts = append(parts, nodeText(cm))
	}
	if inline {
		return strings.Join(parts, "")
	}
	return strings.Join(parts, "\n")
}

// DocFromText is a rich text document of plain text, a paragraph per line.
func DocFromText(text string) map[string]any {
	content := []any{}
	for _, line := range strings.Split(text, "\n") {
		p := map[string]any{"type": "paragraph"}
		if line != "" {
			p["content"] = []any{map[string]any{"type": "text", "text": line}}
		}
		content = append(content, p)
	}
	return map[string]any{"type": "doc", "content": content}
}

// FileRef is a file field's value: an uploaded file in the tenant's
// object store, by id.
type FileRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType,omitempty"`
}

// checkFile checks a file reference; that the file exists, and was
// uploaded by whoever sets it, is the record commands' to check.
func checkFile(v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, want(`a file {"id", "name", "size", "contentType"} as an upload gives it`, v)
	}
	id, _ := m["id"].(string)
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return nil, want("a file id (digits, as a string)", m["id"])
	}
	name, _ := m["name"].(string)
	if name = strings.TrimSpace(name); name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\") {
		return nil, want("a file name", m["name"])
	}
	size, ok := m["size"].(float64)
	if !ok || size < 0 || size != float64(int64(size)) {
		return nil, want("a size in bytes", m["size"])
	}
	out := map[string]any{"id": id, "name": name, "size": int64(size)}
	if ct, _ := m["contentType"].(string); ct != "" {
		out["contentType"] = ct
	}
	return out, nil
}

// FilesOf are the file references a file field (or a list of them) holds.
func FilesOf(f Field, raw json.RawMessage) []FileRef {
	if IsNull(raw) {
		return nil
	}
	var one FileRef
	if f.Type == "file" {
		if json.Unmarshal(raw, &one) == nil && one.ID != "" {
			return []FileRef{one}
		}
		return nil
	}
	if item, many := ItemOf(f); many && item.Type == "file" {
		var out []FileRef
		_ = json.Unmarshal(raw, &out)
		return out
	}
	return nil
}

// VolumeFileRef is a volumeFile field's value.
type VolumeFileRef struct {
	VolumeID string `json:"volumeId"`
	Path     string `json:"path"` // relative to the volume, with '/'
}

func checkVolumeFile(v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, want(`a Volume file {"volumeId", "path"}`, v)
	}
	id, _ := m["volumeId"].(string)
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return nil, want("a volume id (digits, as a string)", m["volumeId"])
	}
	p, _ := m["path"].(string)
	clean := path.Clean(strings.TrimSpace(p))
	if p == "" || path.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(p, "\\") {
		return nil, want("a path inside the volume, such as notes/plan.md", m["path"])
	}
	return map[string]any{"volumeId": id, "path": clean}, nil
}

// SearchText is what a record is searched by: its searchable fields'
// text, a line each (rich text as its text, options by name); empty when
// there is none.
func SearchText(e EntityType, record map[string]json.RawMessage) string {
	var lines []string
	for _, f := range e.Visible() {
		if !f.Searchable || IsNull(record[f.ID]) {
			continue
		}
		var v any
		if json.Unmarshal(record[f.ID], &v) != nil {
			continue
		}
		vf := ValueField(f)
		if items, ok := v.([]any); ok {
			item, _ := ItemOf(vf)
			var parts []string
			for _, it := range items {
				parts = append(parts, textOf(item, it))
			}
			lines = append(lines, strings.Join(parts, ", "))
			continue
		}
		if t := strings.TrimSpace(textOf(vf, v)); t != "" {
			lines = append(lines, t)
		}
	}
	return strings.Join(lines, "\n")
}

// TitleOf is a record's title: its title field's value as text.
func TitleOf(e EntityType, record map[string]json.RawMessage) string {
	f, _, ok := e.FieldByID(e.TitleField)
	if !ok || IsNull(record[f.ID]) {
		return ""
	}
	var v any
	if json.Unmarshal(record[f.ID], &v) != nil {
		return ""
	}
	return textOf(ValueField(f), v)
}
