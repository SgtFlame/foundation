import { useEffect, useRef, useState } from 'react'
import { defaultKeymap, indentWithTab } from '@codemirror/commands'
import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { languages } from '@codemirror/language-data'
import { Compartment, EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { Box, useTheme } from '@mui/material'
import { yCollab, yUndoManagerKeymap } from 'y-codemirror.next'
import * as Y from 'yjs'
import type { CollabSession } from '../collab/CollabSession.ts'
import { useCollabState } from '../collab/useCollabSession.ts'
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

  const closed = state.status === 'closed'
  useEffect(() => {
    view?.dispatch({
      effects: compartments.readOnly.reconfigure(closed ? [EditorState.readOnly.of(true), EditorView.editable.of(false)] : []),
    })
  }, [view, closed, compartments])

  return <Box ref={parent} sx={{ height: '100%' }} />
}

export default LivePreviewView
