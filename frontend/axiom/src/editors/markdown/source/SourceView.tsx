import './monacoSetup.ts'
import { useEffect, useState } from 'react'
import Editor, { type OnMount } from '@monaco-editor/react'
import { useTheme } from '@mui/material'
import { MonacoBinding } from 'y-monaco'
import type { CollabSession } from '../collab/CollabSession.ts'
import { useCollabState } from '../collab/useCollabSession.ts'
import { useRemoteCursorStyles } from './remoteCursors.ts'

type MonacoEditor = Parameters<OnMount>[0]

// The raw markdown in Monaco, bound to the session's shared text
function SourceView({ session }: { session: CollabSession }) {
  const theme = useTheme()
  const state = useCollabState(session)
  const [editor, setEditor] = useState<MonacoEditor>()
  useRemoteCursorStyles(session.awareness)

  useEffect(() => {
    const model = editor?.getModel()
    if (!editor || !model) return
    const binding = new MonacoBinding(session.ytext, model, new Set([editor]), session.awareness)
    return () => binding.destroy()
  }, [editor, session])

  return (
    <Editor
      language="markdown"
      path={`inmemory://markdown/${session.fileId}.md`}
      theme={theme.palette.mode === 'dark' ? 'vs-dark' : 'vs'}
      onMount={setEditor}
      options={{
        wordWrap: 'on',
        minimap: { enabled: false },
        automaticLayout: true,
        scrollBeyondLastLine: false,
        readOnly: state.status === 'closed',
      }}
    />
  )
}

export default SourceView
