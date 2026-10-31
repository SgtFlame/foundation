import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { defaultKeymap, indentWithTab } from '@codemirror/commands'
import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { languages } from '@codemirror/language-data'
import { Compartment, EditorSelection, EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { Box, useTheme } from '@mui/material'
import { yCollab, yUndoManagerKeymap } from 'y-codemirror.next'
import * as Y from 'yjs'
import type { CollabSession } from '../collab/CollabSession.ts'
import { useCollabState } from '../collab/useCollabSession.ts'
import { hasReveal, subscribeReveals, takeReveal } from '../reveal.ts'
import { livePreview } from './livePreview.ts'
import { obsidianTheme } from './obsidianTheme.ts'
import { Tag, WikiLink } from './obsidianSyntax.ts'

// Obsidian-style Live Preview in CodeMirror 6, bound to the session's shared text
function LivePreviewView({ session }: { session: CollabSession }) {
  const theme = useTheme()
  const state = useCollabState(session)
  const parent = useRef<HTMLDivElement>(null)
  const [compartments] = useState(() => ({ theme: new Compartment(), readOnly: new Compartment() }))
  const [view, setView] = useState<EditorView>()

  useEffect(() => {
    if (!parent.current) return
    const created = new EditorView({
      parent: parent.current,
      state: EditorState.create({
        doc: session.ytext.toString(),
        extensions: [
          keymap.of([...yUndoManagerKeymap, ...defaultKeymap, indentWithTab]),
          markdown({ base: markdownLanguage, codeLanguages: languages, extensions: [WikiLink, Tag] }),
          yCollab(session.ytext, session.awareness, { undoManager: new Y.UndoManager(session.ytext) }),
          livePreview(),
          EditorView.lineWrapping,
          EditorView.updateListener.of((u) => {
            if (!u.selectionSet && !u.docChanged) return
            const { from, to, empty } = u.state.selection.main
            session.setSelection(empty ? undefined : [u.state.doc.lineAt(from).number, u.state.doc.lineAt(to).number])
          }),
          compartments.theme.of([]),
          compartments.readOnly.of([]),
        ],
      }),
    })
    setView(created)
    return () => {
      created.destroy()
      setView(undefined)
    }
  }, [session, compartments])

  useEffect(() => {
    view?.dispatch({ effects: compartments.theme.reconfigure(obsidianTheme(theme)) })
  }, [view, theme, compartments])

  // A citation asked for some lines: select them once the document is here
  const revealWaiting = useSyncExternalStore(subscribeReveals, () => hasReveal(session.fileId))
  useEffect(() => {
    if (!view || !state.loaded || !revealWaiting) return
    const reveal = takeReveal(session.fileId)
    if (!reveal) return
    const doc = view.state.doc
    const from = doc.line(Math.min(reveal.from, doc.lines)).from
    const to = doc.line(Math.min(reveal.to, doc.lines)).to
    view.dispatch({ selection: EditorSelection.range(from, to), effects: EditorView.scrollIntoView(from, { y: 'center' }) })
    view.focus()
  }, [view, state.loaded, revealWaiting, session])

  const closed = state.status === 'closed' || state.readOnly
  useEffect(() => {
    view?.dispatch({
      effects: compartments.readOnly.reconfigure(closed ? [EditorState.readOnly.of(true), EditorView.editable.of(false)] : []),
    })
  }, [view, closed, compartments])

  return <Box ref={parent} sx={{ height: '100%' }} />
}

export default LivePreviewView
