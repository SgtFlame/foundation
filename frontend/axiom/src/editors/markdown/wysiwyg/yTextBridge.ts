// Syncs a Milkdown editor with the session's Y.Text through markdown text.
// Milkdown's own collab plugin binds to a Y.XmlFragment, which would split the
// document from the Y.Text the other views share.
//
// Milkdown's serializer normalises markdown (e.g. "*" bullets become "-"), so
// local edits are sent as a patch from `baseline`, the editor's markdown at the
// last sync, to its current markdown. Only what the user changed reaches the
// shared text; the rest keeps everyone else's formatting.

import { editorViewCtx, parserCtx } from '@milkdown/kit/core'
import type { Editor } from '@milkdown/kit/core'
import { TextSelection } from '@milkdown/kit/prose/state'
import { type Node, Slice } from '@milkdown/kit/prose/model'
import { getMarkdown } from '@milkdown/kit/utils'
import DiffMatchPatch from 'diff-match-patch'
import type * as Y from 'yjs'

const BRIDGE_ORIGIN = Symbol('wysiwyg-bridge')
const remoteDelayMs = 100

const dmp = new DiffMatchPatch()

// Rewrites text to target with minimal inserts and deletes
function applyDiff(text: Y.Text, target: string) {
  let pos = 0
  for (const [op, chunk] of dmp.diff_main(text.toString(), target)) {
    if (op === DiffMatchPatch.DIFF_EQUAL) {
      pos += chunk.length
    } else if (op === DiffMatchPatch.DIFF_DELETE) {
      text.delete(pos, chunk.length)
    } else {
      text.insert(pos, chunk)
      pos += chunk.length
    }
  }
}

// The selection as (textblock index, offset), which survives reloading the document
function blockPosition(doc: Node, pos: number): [number, number] {
  let index = -1
  let result: [number, number] = [0, 0]
  doc.descendants((node, start) => {
    if (!node.isTextblock) return true
    index++
    if (pos >= start + 1 && pos <= start + 1 + node.content.size) result = [index, pos - start - 1]
    return false
  })
  return result
}

function fromBlockPosition(doc: Node, [block, offset]: [number, number]): number {
  let index = -1
  let result = 1
  doc.descendants((node, start) => {
    if (index >= block) return false
    if (!node.isTextblock) return true
    index++
    result = start + 1 + Math.min(offset, node.content.size)
    return false
  })
  return Math.min(result, doc.content.size)
}

export class YTextBridge {
  private readonly editor: Editor
  private readonly ytext: Y.Text
  private baseline: string // the editor's markdown when it last matched the shared text
  private lastApplied: string // the shared text the editor last loaded or produced
  private remoteTimer?: ReturnType<typeof setTimeout>

  constructor(editor: Editor, ytext: Y.Text) {
    this.editor = editor
    this.ytext = ytext
    this.baseline = editor.action(getMarkdown())
    this.lastApplied = ytext.toString()
    ytext.observe(this.onRemote)
    // The text may have changed while the editor was being created
    this.applyRemote()
  }

  // Milkdown's (already debounced) markdownUpdated
  onLocal = (markdown: string) => {
    if (markdown !== this.baseline) this.pushLocal(markdown)
  }

  destroy() {
    clearTimeout(this.remoteTimer)
    this.ytext.unobserve(this.onRemote)
  }

  private pushLocal(markdown: string) {
    const patches = dmp.patch_make(this.baseline, markdown)
    const [result] = dmp.patch_apply(patches, this.ytext.toString())
    this.ytext.doc?.transact(() => applyDiff(this.ytext, result), BRIDGE_ORIGIN)
    this.baseline = markdown
    this.lastApplied = result
  }

  private readonly onRemote = (_event: Y.YTextEvent, transaction: Y.Transaction) => {
    if (transaction.origin === BRIDGE_ORIGIN) return
    clearTimeout(this.remoteTimer)
    this.remoteTimer = setTimeout(() => this.applyRemote(), remoteDelayMs)
  }

  private applyRemote() {
    // Send any local edit the listener has not reported yet, so reloading keeps it
    const current = this.editor.action(getMarkdown())
    if (current !== this.baseline) this.pushLocal(current)

    const markdown = this.ytext.toString()
    if (markdown === this.lastApplied) return
    this.editor.action((ctx) => {
      const view = ctx.get(editorViewCtx)
      const parsed = ctx.get(parserCtx)(markdown)
      if (!parsed) return
      const { state } = view
      const anchor = blockPosition(state.doc, state.selection.from)
      // Kept out of the undo history, so undo only reverts this user's edits
      const tr = state.tr.replace(0, state.doc.content.size, new Slice(parsed.content, 0, 0))
      tr.setSelection(TextSelection.create(tr.doc, fromBlockPosition(tr.doc, anchor)))
      view.dispatch(tr.setMeta('addToHistory', false))
    })
    this.baseline = this.editor.action(getMarkdown())
    this.lastApplied = markdown
  }
}
