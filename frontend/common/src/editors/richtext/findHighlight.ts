// Marks every find match in the editor, the current one stronger
// (forge 35b-1): a ProseMirror plugin whose matches the find panel sets
// with a transaction's meta, mapped through edits until it sets them again.

import { Extension } from '@tiptap/core'
import { Plugin, PluginKey, type EditorState } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'
import type { Range } from './find.ts'

interface FindState {
  matches: Range[]
  current: number
  decorations: DecorationSet
}

export const findKey = new PluginKey<FindState>('richtextFind')

function decorate(state: EditorState, matches: Range[], current: number): DecorationSet {
  return DecorationSet.create(state.doc, matches.map((m, i) => Decoration.inline(m.from, m.to, { class: i === current ? 'rt-find rt-find-current' : 'rt-find' })))
}

export const FindHighlight = Extension.create({
  name: 'richtextFind',
  addProseMirrorPlugins() {
    return [
      new Plugin<FindState>({
        key: findKey,
        state: {
          init: () => ({ matches: [], current: -1, decorations: DecorationSet.empty }),
          apply(tr, value, _old, state) {
            const meta = tr.getMeta(findKey) as { matches: Range[]; current: number } | undefined
            if (meta) return { ...meta, decorations: decorate(state, meta.matches, meta.current) }
            if (!tr.docChanged) return value
            return { ...value, decorations: value.decorations.map(tr.mapping, tr.doc) }
          },
        },
        props: {
          decorations: (state) => findKey.getState(state)?.decorations,
        },
      }),
    ]
  },
})
