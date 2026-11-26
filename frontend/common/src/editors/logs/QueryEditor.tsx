import '../markdown/source/monacoSetup.ts'
import { useEffect, useRef, useState } from 'react'
import Editor, { type OnMount } from '@monaco-editor/react'
import { useTheme } from '@mui/material'
import * as monaco from 'monaco-editor'
import type { QueryError } from '../../services/Logs.ts'
import { languageId, markError, provideCompletion, registerLogsQL, type CompletionSource } from './logsql.ts'

type MonacoEditor = Parameters<OnMount>[0]

let nextModel = 1

interface QueryEditorProps {
  value: string
  onChange: (value: string) => void
  onRun: () => void // Ctrl+Enter
  error?: QueryError
  completion: CompletionSource
}

// The raw LogsQL query in Monaco
function QueryEditor({ value, onChange, onRun, error, completion }: QueryEditorProps) {
  const theme = useTheme()
  const [editor, setEditor] = useState<MonacoEditor>()
  const [path] = useState(() => `inmemory://logs/query-${nextModel++}.logsql`)
  const latest = useRef({ onRun, completion })
  latest.current = { onRun, completion }
  registerLogsQL()

  useEffect(() => {
    const model = editor?.getModel()
    if (!editor || !model) return
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => latest.current.onRun())
    return provideCompletion(model, {
      fieldNames: () => latest.current.completion.fieldNames(),
      fieldValues: (field) => latest.current.completion.fieldValues(field),
      streamValues: (field) => latest.current.completion.streamValues(field),
    })
  }, [editor])

  useEffect(() => {
    const model = editor?.getModel()
    if (model) markError(model, error)
  }, [editor, error])

  return (
    <Editor
      height={72}
      language={languageId}
      path={path}
      value={value}
      onChange={(text) => onChange(text ?? '')}
      onMount={setEditor}
      theme={theme.palette.mode === 'dark' ? 'vs-dark' : 'vs'}
      options={{
        wordWrap: 'on',
        minimap: { enabled: false },
        lineNumbers: 'off',
        folding: false,
        glyphMargin: false,
        lineDecorationsWidth: 4,
        scrollBeyondLastLine: false,
        automaticLayout: true,
        fontSize: 13,
        renderLineHighlight: 'none',
        overviewRulerLanes: 0,
        fixedOverflowWidgets: true,
        // Typing ( or a space must never accept a suggestion
        acceptSuggestionOnCommitCharacter: false,
      }}
    />
  )
}

export default QueryEditor
