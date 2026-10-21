// Obsidian-like styling for the Live Preview, from the MUI theme

import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { EditorView } from '@codemirror/view'
import { tags as t } from '@lezer/highlight'
import { alpha, type Theme } from '@mui/material'

const calloutColors = (theme: Theme): Record<string, string> => ({
  note: theme.palette.primary.main,
  info: theme.palette.info.main,
  tip: theme.palette.success.main,
  success: theme.palette.success.main,
  question: theme.palette.warning.light,
  warning: theme.palette.warning.main,
  danger: theme.palette.error.main,
  error: theme.palette.error.main,
  bug: theme.palette.error.main,
  example: theme.palette.secondary.main,
  quote: theme.palette.text.secondary,
})

const mono = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'

export function obsidianTheme(theme: Theme) {
  const { palette } = theme
  const dark = palette.mode === 'dark'
  const codeBackground = alpha(palette.text.primary, 0.06)

  const callouts = Object.fromEntries(
    Object.entries(calloutColors(theme)).flatMap(([type, color]) => [
      [`.cm-lp-callout-${type}`, { borderLeftColor: color, backgroundColor: alpha(color, 0.08) }],
      [`.cm-lp-callout-${type}.cm-lp-callout-title`, { color }],
    ]),
  )

  const editorTheme = EditorView.theme(
    {
      '&': { height: '100%', color: palette.text.primary, backgroundColor: palette.background.default },
      '&.cm-focused': { outline: 'none' },
      '.cm-scroller': { fontFamily: theme.typography.fontFamily, lineHeight: 1.6, fontSize: '16px' },
      // A readable line width, centred like Obsidian's
      '.cm-content': { maxWidth: '750px', margin: '0 auto', padding: '24px 16px' },
      '.cm-line': { padding: '0 4px' },
      '.cm-cursor': { borderLeftColor: palette.text.primary },
      '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
        backgroundColor: alpha(palette.primary.main, 0.2),
      },
      '.cm-lp-h1': { fontSize: '1.9em', fontWeight: 700, lineHeight: 1.3, paddingTop: '0.4em' },
      '.cm-lp-h2': { fontSize: '1.6em', fontWeight: 700, lineHeight: 1.3, paddingTop: '0.3em' },
      '.cm-lp-h3': { fontSize: '1.35em', fontWeight: 600, paddingTop: '0.2em' },
      '.cm-lp-h4': { fontSize: '1.2em', fontWeight: 600 },
      '.cm-lp-h5': { fontSize: '1.1em', fontWeight: 600 },
      '.cm-lp-h6': { fontSize: '1em', fontWeight: 600, color: palette.text.secondary },
      '.cm-lp-quote': {
        borderLeft: `3px solid ${palette.divider}`,
        paddingLeft: '14px',
        color: palette.text.secondary,
      },
      '.cm-lp-callout': { borderLeft: '3px solid', paddingLeft: '14px' },
      '.cm-lp-callout-title': { fontWeight: 600 },
      '.cm-lp-callout-label': { marginRight: '4px' },
      ...callouts,
      '.cm-lp-codeblock': { fontFamily: mono, fontSize: '0.9em', backgroundColor: codeBackground },
      '.cm-lp-link, .cm-lp-wikilink': { color: palette.primary.main, textDecoration: 'underline', cursor: 'pointer' },
      '.cm-lp-tag': {
        color: palette.primary.main,
        backgroundColor: alpha(palette.primary.main, 0.1),
        borderRadius: '10px',
        padding: '1px 6px',
      },
      '.cm-lp-bullet': { color: palette.text.secondary, display: 'inline-block', width: '1ch' },
      '.cm-lp-checkbox': { margin: '0 6px 0 0', verticalAlign: 'middle', cursor: 'pointer' },
      '.cm-lp-task-done': { color: palette.text.secondary, textDecoration: 'line-through' },
      '.cm-lp-hr': { border: 'none', borderTop: `1px solid ${palette.divider}`, margin: '0.6em 0' },
      '.cm-lp-image': { maxWidth: '100%', verticalAlign: 'middle' },
    },
    { dark },
  )

  const highlight = HighlightStyle.define([
    { tag: t.heading, fontWeight: 'inherit' },
    { tag: t.strong, fontWeight: 700 },
    { tag: t.emphasis, fontStyle: 'italic' },
    { tag: t.strikethrough, textDecoration: 'line-through' },
    { tag: t.monospace, fontFamily: mono, fontSize: '0.9em', backgroundColor: codeBackground, borderRadius: '3px' },
    { tag: t.link, color: palette.primary.main },
    { tag: t.url, color: palette.text.secondary },
    { tag: [t.processingInstruction, t.meta, t.contentSeparator], color: palette.text.disabled },
    { tag: t.labelName, color: palette.primary.main },
    // Fenced code
    { tag: t.keyword, color: dark ? '#c792ea' : '#7c4dff' },
    { tag: [t.string, t.special(t.string)], color: dark ? '#c3e88d' : '#2e7d32' },
    { tag: t.comment, color: palette.text.disabled, fontStyle: 'italic' },
    { tag: [t.number, t.bool, t.null], color: dark ? '#f78c6c' : '#d84315' },
    { tag: [t.typeName, t.className], color: dark ? '#ffcb6b' : '#ef6c00' },
    { tag: [t.function(t.variableName), t.definition(t.variableName)], color: dark ? '#82aaff' : '#1565c0' },
  ])

  return [editorTheme, syntaxHighlighting(highlight)]
}
