import { useEffect, useRef, useState } from 'react'
import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { languages } from '@codemirror/language-data'
import { Compartment, EditorState, Prec } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { alpha, Box, useTheme } from '@mui/material'
import { livePreview } from '../../editors/markdown/live/livePreview.ts'
import { Tag, WikiLink } from '../../editors/markdown/live/obsidianSyntax.ts'
import { obsidianTheme } from '../../editors/markdown/live/obsidianTheme.ts'
import { citationChips, markCitations, type Citations } from './citations.ts'

// Read-only markdown rendered by Live Preview's renderer, so code blocks,
// links and callouts look as they do in the Markdown editor. Text that grows
// (a streaming reply) is appended rather than replaced. With citations, a
// valid [n] is a chip that calls onCite(n).
function MarkdownText({ text, citations }: { text: string; citations?: Citations }) {
  const theme = useTheme()
  const parent = useRef<HTMLDivElement>(null)
  const [themeCompartment] = useState(() => new Compartment())
  const [view, setView] = useState<EditorView>()
  const cites = useRef(citations)
  useEffect(() => {
    cites.current = citations
  })

  useEffect(() => {
    if (!parent.current) return
    const created = new EditorView({
      parent: parent.current,
      state: EditorState.create({
        extensions: [
          markdown({ base: markdownLanguage, codeLanguages: languages, extensions: [WikiLink, Tag] }),
          livePreview({ revealSelection: false }),
          EditorView.lineWrapping,
          EditorState.readOnly.of(true),
          EditorView.editable.of(false),
          themeCompartment.of([]),
          citationChips(() => cites.current ?? { titles: [], onCite: () => {} }),
          // Sized by its text and laid out by the transcript, unlike the editor
          Prec.highest(
            EditorView.theme({
              '&': { height: 'auto', backgroundColor: 'transparent' },
              '.cm-content': { maxWidth: 'none', margin: 0, padding: 0 },
              '.cm-line': { padding: 0 },
            }),
          ),
        ],
      }),
    })
    setView(created)
    return () => {
      created.destroy()
      setView(undefined)
    }
  }, [themeCompartment])

  useEffect(() => {
    view?.dispatch({ effects: themeCompartment.reconfigure(obsidianTheme(theme)) })
  }, [view, theme, themeCompartment])

  const shown = citations ? markCitations(text, citations.titles.length) : text
  useEffect(() => {
    if (!view) return
    const text = shown
    const current = view.state.doc.toString()
    if (current === text) return
    view.dispatch(
      text.startsWith(current)
        ? { changes: { from: current.length, insert: text.slice(current.length) } }
        : { changes: { from: 0, to: current.length, insert: text } },
    )
  }, [view, shown])

  return (
    <Box
      ref={parent}
      sx={{
        '& .cm-editor.cm-focused': { outline: 'none' },
        '--cite-color': theme.palette.primary.main,
        '--cite-bg': alpha(theme.palette.primary.main, 0.12),
      }}
    />
  )
}

export default MarkdownText
