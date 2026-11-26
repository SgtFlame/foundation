// doc.markdown's highlights in CodeMirror (forge 18c): marks placed by
// placeHighlights, mapped through every change (typing here, and others'
// typing arriving through the room), placed again when the prop changes;
// a click on one reports its id, and a selection is reported once the
// mouse or keys settle.

import { RangeSetBuilder, StateEffect, StateField, type Extension } from '@codemirror/state'
import { Decoration, EditorView, type DecorationSet } from '@codemirror/view'
import type { Placed } from './highlights.ts'

// The highlights wanted, and how to place them in a text: kept so that
// ones not found yet (the room's text had not arrived) are placed again as
// the text changes
export interface HighlightSet {
  want: number
  place(text: string): Placed[]
}

export const setHighlights = StateEffect.define<HighlightSet>()

function build(list: Placed[], length: number): DecorationSet {
  const b = new RangeSetBuilder<Decoration>()
  for (const h of list) {
    const from = Math.max(0, Math.min(h.from, length))
    const to = Math.max(from, Math.min(h.to, length))
    if (to <= from) continue
    b.add(from, to, Decoration.mark({
      class: `cm-doc-highlight cm-doc-highlight-${h.kind}`,
      attributes: { 'data-highlight': h.id, style: `background-color: ${h.colour}33; border-bottom: 2px solid ${h.colour}; cursor: pointer` },
    }))
  }
  return b.finish()
}

interface Marks {
  deco: DecorationSet
  placed: number
  set?: HighlightSet
}

export const highlightField = StateField.define<Marks>({
  create: () => ({ deco: Decoration.none, placed: 0 }),
  update(value, tr) {
    let next: Marks = { ...value, deco: value.deco.map(tr.changes) }
    let placeAgain = tr.docChanged && !!value.set && value.placed < value.set.want
    for (const e of tr.effects) {
      if (e.is(setHighlights)) {
        next = { ...next, set: e.value }
        placeAgain = true
      }
    }
    if (placeAgain && next.set) {
      const list = next.set.place(tr.state.doc.toString())
      next = { ...next, deco: build(list, tr.state.doc.length), placed: list.length }
    }
    return next
  },
  provide: (f) => EditorView.decorations.from(f, (m) => m.deco),
})

// Where a highlight is now, by its id
export function highlightRange(view: EditorView, id: string): { from: number; to: number } | undefined {
  let found: { from: number; to: number } | undefined
  view.state.field(highlightField, false)?.deco.between(0, view.state.doc.length, (from, to, deco) => {
    if (deco.spec.attributes?.['data-highlight'] === id) {
      found = { from, to }
      return false
    }
  })
  return found
}

export interface HighlightHandlers {
  clicked(id: string): void
  selected(from: number, to: number): void
}

export function highlightExtension(handlers: { current: HighlightHandlers }): Extension {
  let timer: ReturnType<typeof setTimeout> | undefined
  let last = ''
  return [
    highlightField,
    EditorView.domEventHandlers({
      click(e) {
        const el = (e.target as HTMLElement | null)?.closest?.('[data-highlight]')
        const id = el?.getAttribute('data-highlight')
        // a click that made a selection is a selection, not a click
        if (id && (window.getSelection()?.isCollapsed ?? true)) handlers.current.clicked(id)
        return false
      },
    }),
    EditorView.updateListener.of((u) => {
      if (!u.selectionSet && !u.docChanged) return
      const r = u.state.selection.main
      const key = r.empty ? 'empty' : `${r.from}:${r.to}`
      // a caret moving as someone types is not a new selection
      if (key === last || (r.empty && last === '')) return
      clearTimeout(timer)
      timer = setTimeout(() => {
        last = key === 'empty' ? '' : key
        handlers.current.selected(r.from, r.to)
      }, 350)
    }),
  ]
}
