package prosemirror

import "strings"

// PlainText is a document's text without markup: a line per block, a
// table's cells separated by tabs, a code block's text as it is, and the
// footnotes' text at the end. records.PlainText reads a record's
// richText the same way, more loosely.
func PlainText(doc Node) string {
	p := &plainer{}
	lines := p.blocks(doc.Content)
	lines = append(lines, p.footnotes...)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type plainer struct {
	footnotes []string
}

func (p *plainer) blocks(nodes []Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, p.block(n)...)
	}
	return out
}

func (p *plainer) block(n Node) []string {
	switch n.Type {
	case "table":
		var rows []string
		for _, row := range n.Content {
			var cells []string
			for _, cell := range row.Content {
				cells = append(cells, strings.Join(p.blocks(cell.Content), " "))
			}
			rows = append(rows, strings.Join(cells, "\t"))
		}
		return rows
	case "codeBlock", "code_block":
		return []string{plain(n)}
	case "horizontalRule", "horizontal_rule", "pageBreak":
		return nil
	case "image":
		if alt := n.attr("alt"); alt != "" {
			return []string{alt}
		}
		return nil
	case "mathBlock", "math_display", "blockMath":
		return []string{latex(n)}
	}
	if len(n.Content) == 0 {
		if n.Type == "paragraph" {
			return []string{""}
		}
		return nil
	}
	if isInline(n.Content[0]) {
		return []string{p.inline(n.Content)}
	}
	return p.blocks(n.Content)
}

func (p *plainer) inline(nodes []Node) string {
	var b strings.Builder
	for _, n := range nodes {
		switch n.Type {
		case "text":
			b.WriteString(n.Text)
		case "hardBreak", "hard_break":
			b.WriteString("\n")
		case "mathInline", "math_inline", "inlineMath":
			b.WriteString(latex(n))
		case "footnote", "footnoteReference":
			text := p.inline(n.Content)
			if text == "" {
				text = n.attr("text")
			}
			p.footnotes = append(p.footnotes, text)
		case "image":
			b.WriteString(n.attr("alt"))
		default:
			if len(n.Content) > 0 {
				b.WriteString(p.inline(n.Content))
			} else {
				b.WriteString(atomText(n))
			}
		}
	}
	return b.String()
}

// plain is a node's text, its children's concatenated.
func plain(n Node) string {
	if n.Type == "text" {
		return n.Text
	}
	if n.Type == "hardBreak" || n.Type == "hard_break" {
		return "\n"
	}
	var b strings.Builder
	for _, c := range n.Content {
		b.WriteString(plain(c))
	}
	return b.String()
}
