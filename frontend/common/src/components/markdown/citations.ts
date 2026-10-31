// Inline citations [n] in a read-only MarkdownText. Markdown would parse
// "[1]" and "[1][3]" as links, which Live Preview renders without their
// brackets, so the text is given to the editor with each valid [n] as ⟦n⟧,
// which this extension shows as a clickable chip.

import { RangeSetBuilder } from '@codemirror/state'
import { Decoration, EditorView, ViewPlugin, WidgetType, type DecorationSet, type ViewUpdate } from '@codemirror/view'

export interface Citations {
  titles: readonly string[] // [n] is titles[n - 1]; other numbers stay plain text
  onCite: (n: number) => void
}

const open = '⟦'
const close = '⟧'
const token = /⟦(\d{1,3})⟧/g

// The text with every citation of a known number as ⟦n⟧
export function markCitations(text: string, count: number): string {
  return text.replace(/\[(\d{1,3})\](?!\()/g, (all, n: string) => (Number(n) >= 1 && Number(n) <= count ? `${open}${n}${close}` : all))
}

class CitationWidget extends WidgetType {
  readonly n: number
  readonly title: string

  constructor(n: number, title: string) {
    super()
    this.n = n
    this.title = title
  }

  eq(other: CitationWidget) {
    return other.n === this.n && other.title === this.title
  }

  toDOM() {
    const el = document.createElement('sup')
    el.className = 'cm-citation'
    el.textContent = String(this.n)
    el.dataset.cite = String(this.n)
    el.title = this.title
    el.setAttribute('role', 'link')
    el.setAttribute('aria-label', `Source ${this.n}: ${this.title}`)
    return el
  }

  ignoreEvent() {
    return false
  }
}

function build(view: EditorView, titles: readonly string[]): DecorationSet {
  const builder = new RangeSetBuilder<Decoration>()
  for (const { from, to } of view.visibleRanges) {
    const text = view.state.sliceDoc(from, to)
    for (const m of text.matchAll(token)) {
      const n = Number(m[1])
      builder.add(from + m.index, from + m.index + m[0].length, Decoration.replace({ widget: new CitationWidget(n, titles[n - 1] ?? '') }))
    }
  }
  return builder.finish()
}

// The chips and their clicks; get() reads the current titles and handler
export function citationChips(get: () => Citations) {
  return [
    ViewPlugin.fromClass(
      class {
        decorations: DecorationSet
        constructor(view: EditorView) {
          this.decorations = build(view, get().titles)
        }
        update(u: ViewUpdate) {
          if (u.docChanged || u.viewportChanged) this.decorations = build(u.view, get().titles)
        }
      },
      { decorations: (p) => p.decorations },
    ),
    EditorView.domEventHandlers({
      mousedown(event) {
        const chip = (event.target as HTMLElement).closest<HTMLElement>('.cm-citation')
        if (!chip?.dataset.cite) return false
        event.preventDefault()
        get().onCite(Number(chip.dataset.cite))
        return true
      },
    }),
    EditorView.theme({
      '.cm-citation': {
        cursor: 'pointer',
        padding: '0 4px',
        margin: '0 1px',
        borderRadius: '4px',
        fontSize: '0.75em',
        fontWeight: 600,
        color: 'var(--cite-color)',
        backgroundColor: 'var(--cite-bg)',
      },
      '.cm-citation:hover': { textDecoration: 'underline' },
    }),
  ]
}
