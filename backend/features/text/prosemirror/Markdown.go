package prosemirror

import (
	"fmt"
	"strconv"
	"strings"
)

// Block is where one top-level block of a document lands in its derived
// Markdown: lines from 1, inclusive. A citation's lines name blocks this
// way, and the editor opens the document at the block (forge 35b-1).
type Block struct {
	Index     int    `json:"index"`        // its position among the doc's children
	ID        string `json:"id,omitempty"` // its id attribute, when the editor set one
	Type      string `json:"type"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// Derived is a document's Markdown and where each top-level block is in it.
type Derived struct {
	Markdown string
	Blocks   []Block
}

// BlockAt is the block holding a line of the derived Markdown, or false
// for a blank line between blocks or past the end (footnotes).
func (d Derived) BlockAt(line int) (Block, bool) {
	for _, b := range d.Blocks {
		if line >= b.StartLine && line <= b.EndLine {
			return b, true
		}
	}
	return Block{}, false
}

// Markdown is a document's Markdown: headings, paragraphs, lists (bulleted,
// numbered with their start, checklists), block quotes, code blocks, rules,
// tables (the first row as the header), images, links, equations ($…$)
// and footnotes ([^n], defined at the end). A mark Markdown cannot hold
// (underline, colour, highlight, font, super- and subscript) leaves its
// text plain, and an unknown node gives its children's text.
func Markdown(doc Node) string {
	return Derive(doc).Markdown
}

// Derive is Markdown with the lines of each top-level block.
func Derive(doc Node) Derived {
	r := &renderer{}
	var out []string
	var blocks []Block
	line := 1
	for i, n := range doc.Content {
		s := r.block(n)
		if s == "" {
			continue
		}
		if len(out) > 0 {
			line++ // the blank line between blocks
		}
		count := strings.Count(s, "\n") + 1
		blocks = append(blocks, Block{Index: i, ID: n.ID(), Type: n.Type, StartLine: line, EndLine: line + count - 1})
		line += count
		out = append(out, s)
	}
	for i, f := range r.footnotes {
		if i == 0 {
			out = append(out, "")
		}
		out[len(out)-1] += fmt.Sprintf("[^%d]: %s", i+1, f)
		if i < len(r.footnotes)-1 {
			out[len(out)-1] += "\n"
		}
	}
	md := strings.Join(out, "\n\n")
	if md != "" {
		md += "\n"
	}
	return Derived{Markdown: md, Blocks: blocks}
}

type renderer struct {
	footnotes []string
}

// blocks renders sibling blocks separated by a blank line.
func (r *renderer) blocks(nodes []Node, sep string) string {
	var parts []string
	for _, n := range nodes {
		if s := r.block(n); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, sep)
}

func (r *renderer) block(n Node) string {
	switch n.Type {
	case "paragraph":
		return r.inline(n.Content)
	case "heading", "title":
		level := min(max(n.intAttr("level", 1), 1), 6)
		if n.Type == "title" {
			level = 1
		}
		return strings.Repeat("#", level) + " " + strings.ReplaceAll(r.inline(n.Content), "\n", " ")
	case "blockquote":
		return prefixLines(r.blocks(n.Content, "\n\n"), "> ", ">")
	case "codeBlock", "code_block":
		code := plain(n)
		fence := "```"
		for strings.Contains(code, fence) {
			fence += "`"
		}
		return fence + n.attr("language") + "\n" + code + "\n" + fence
	case "horizontalRule", "horizontal_rule", "pageBreak", "sectionBreak":
		return "---"
	case "tableOfContents":
		// Rebuilt from the headings where it is shown (forge 35b-2)
		return ""
	case "bulletList", "bullet_list":
		return r.list(n, func(int, Node) string { return "- " })
	case "orderedList", "ordered_list":
		start := n.intAttr("start", 1)
		return r.list(n, func(i int, _ Node) string { return strconv.Itoa(start+i) + ". " })
	case "taskList":
		return r.list(n, func(_ int, item Node) string {
			if checked, _ := item.Attrs["checked"].(bool); checked {
				return "- [x] "
			}
			return "- [ ] "
		})
	case "table":
		return r.table(n)
	case "image":
		return image(n)
	case "mathBlock", "math_display", "blockMath":
		return "$$\n" + latex(n) + "\n$$"
	case "text":
		return r.inline([]Node{n})
	}
	if len(n.Content) == 0 {
		return ""
	}
	if isInline(n.Content[0]) {
		return r.inline(n.Content)
	}
	return r.blocks(n.Content, "\n\n")
}

// list renders a list's items, tight, each item's later lines indented
// under its marker.
func (r *renderer) list(n Node, marker func(i int, item Node) string) string {
	var items []string
	for i, item := range n.Content {
		m := marker(i, item)
		body := r.blocks(item.Content, "\n")
		items = append(items, m+indent(body, strings.Repeat(" ", len(m))))
	}
	return strings.Join(items, "\n")
}

func (r *renderer) table(n Node) string {
	var rows [][]string
	width := 0
	for _, row := range n.Content {
		var cells []string
		for _, cell := range row.Content {
			text := strings.ReplaceAll(r.blocks(cell.Content, " "), "\n", " ")
			cells = append(cells, strings.ReplaceAll(text, "|", `\|`))
		}
		width = max(width, len(cells))
		rows = append(rows, cells)
	}
	if len(rows) == 0 || width == 0 {
		return ""
	}
	line := func(cells []string) string {
		for len(cells) < width {
			cells = append(cells, "")
		}
		return "| " + strings.Join(cells, " | ") + " |"
	}
	out := []string{line(rows[0]), line(repeat("---", width))}
	for _, row := range rows[1:] {
		out = append(out, line(row))
	}
	return strings.Join(out, "\n")
}

// inline renders inline content: text with its marks, breaks, images,
// equations, footnotes and atoms such as mentions.
func (r *renderer) inline(nodes []Node) string {
	var b strings.Builder
	for _, n := range nodes {
		switch n.Type {
		case "text":
			b.WriteString(marked(n))
		case "hardBreak", "hard_break":
			b.WriteString("\\\n")
		case "image":
			b.WriteString(image(n))
		case "mathInline", "math_inline", "inlineMath":
			b.WriteString("$" + latex(n) + "$")
		case "pageNumber":
			// A header's or footer's field, never body text (forge 35b-2)
		case "footnote", "footnoteReference":
			text := r.inline(n.Content)
			if text == "" {
				text = n.attr("text")
			}
			r.footnotes = append(r.footnotes, strings.ReplaceAll(text, "\n", " "))
			fmt.Fprintf(&b, "[^%d]", len(r.footnotes))
		default:
			if len(n.Content) > 0 {
				b.WriteString(r.inline(n.Content))
			} else {
				b.WriteString(escape(atomText(n)))
			}
		}
	}
	return b.String()
}

// marked is a text node with the marks Markdown holds; spaces at its ends
// stay outside the markers, which Markdown would not read inside them.
func marked(n Node) string {
	text := n.Text
	for _, m := range n.Marks {
		if m.Type == "code" {
			fence := "`"
			for strings.Contains(text, fence) {
				fence += "`"
			}
			pad := ""
			if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
				pad = " "
			}
			return wrapLink(n, fence+pad+text+pad+fence)
		}
	}
	core := strings.TrimSpace(text)
	if core == "" {
		return text
	}
	lead := text[:strings.Index(text, core)]
	trail := text[len(lead)+len(core):]
	s := escape(core)
	for _, m := range n.Marks {
		switch m.Type {
		case "bold", "strong":
			s = "**" + s + "**"
		case "italic", "em":
			s = "_" + s + "_"
		case "strike", "strikethrough":
			s = "~~" + s + "~~"
		}
	}
	return lead + wrapLink(n, s) + trail
}

func wrapLink(n Node, s string) string {
	for _, m := range n.Marks {
		if m.Type == "link" {
			if href, _ := m.Attrs["href"].(string); href != "" {
				return "[" + s + "](" + strings.ReplaceAll(href, " ", "%20") + ")"
			}
		}
	}
	return s
}

// markdownSpecial are the characters escaped in text, so that text never
// turns into markup it did not hold.
var markdownSpecial = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`)

func escape(s string) string { return markdownSpecial.Replace(s) }

func image(n Node) string {
	src := strings.ReplaceAll(n.attr("src"), " ", "%20")
	if title := n.attr("title"); title != "" {
		return "![" + escape(n.attr("alt")) + "](" + src + ` "` + strings.ReplaceAll(title, `"`, `'`) + `")`
	}
	return "![" + escape(n.attr("alt")) + "](" + src + ")"
}

func latex(n Node) string {
	if s := n.attr("latex"); s != "" {
		return s
	}
	return plain(n)
}

// atomText is what an inline node without content shows: a mention's
// label, a chip's text.
func atomText(n Node) string {
	for _, a := range []string{"label", "text", "title", "name", "id"} {
		if s := n.attr(a); s != "" {
			if n.Type == "mention" && a != "text" {
				return "@" + s
			}
			return s
		}
	}
	return ""
}

func isInline(n Node) bool {
	switch n.Type {
	case "text", "hardBreak", "hard_break", "mathInline", "math_inline", "inlineMath", "footnote", "footnoteReference", "mention", "pageNumber":
		return true
	}
	return false
}

// indent indents every line but the first.
func indent(s, pad string) string {
	return strings.ReplaceAll(s, "\n", "\n"+pad)
}

// prefixLines puts prefix before every line, empty for blank ones.
func prefixLines(s, prefix, empty string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = empty
		} else {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}
