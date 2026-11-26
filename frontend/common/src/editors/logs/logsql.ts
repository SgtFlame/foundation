// LogsQL in Monaco: a Monarch tokenizer, completion of field names, values,
// pipes and stream fields, and markers for the server's parse errors.

import * as monaco from 'monaco-editor'
import type { QueryError, ValueHits } from '../../services/Logs.ts'
import { fieldName } from './view.ts'

export const languageId = 'logsql'

// What completion asks for, per editor model
export interface CompletionSource {
  fieldNames: () => Promise<string[]>
  fieldValues: (field: string) => Promise<ValueHits[]>
  streamValues: (field: string) => Promise<ValueHits[]>
}

const sources = new Map<string, CompletionSource>()

// Completion in model uses source until the returned function is called
export function provideCompletion(model: monaco.editor.ITextModel, source: CompletionSource): () => void {
  const key = model.uri.toString()
  sources.set(key, source)
  return () => {
    if (sources.get(key) === source) sources.delete(key)
  }
}

export const pipes = [
  'copy', 'delete', 'drop_empty_fields', 'extract', 'extract_regexp', 'facets', 'field_names', 'field_values', 'fields',
  'filter', 'first', 'format', 'join', 'last', 'len', 'limit', 'math', 'offset', 'pack_json', 'pack_logfmt', 'rename',
  'replace', 'replace_regexp', 'sample', 'sort', 'stats', 'stream_context', 'top', 'uniq', 'unpack_json', 'unpack_logfmt',
  'unroll',
]

const statsFunctions = ['count', 'count_uniq', 'sum', 'avg', 'min', 'max', 'median', 'quantile', 'uniq_values', 'values', 'rate']

const streamFields = ['app', 'source', 'instance', 'tenant.id']

let registered = false

// Registers the language once
export function registerLogsQL() {
  if (registered) return
  registered = true
  monaco.languages.register({ id: languageId })
  monaco.languages.setLanguageConfiguration(languageId, {
    comments: { lineComment: '#' },
    brackets: [
      ['{', '}'],
      ['(', ')'],
      ['[', ']'],
    ],
    autoClosingPairs: [
      { open: '{', close: '}' },
      { open: '(', close: ')' },
      { open: '[', close: ']' },
      { open: '"', close: '"', notIn: ['string'] },
    ],
  })
  monaco.languages.setMonarchTokensProvider(languageId, {
    ignoreCase: true,
    keywords: ['and', 'or', 'not', 'by', 'in', 'range', 'desc', 'asc', 'as', 'if'],
    pipes,
    tokenizer: {
      root: [
        [/#.*$/, 'comment'],
        [/"/, 'string', '@dstring'],
        [/'/, 'string', '@sstring'],
        [/`/, 'string', '@bstring'],
        [/\|\s*/, { token: 'delimiter', next: '@pipe' }],
        [/[{}()[\]]/, '@brackets'],
        [/:=|:~|:>=|:<=|:>|:<|:|!=|=~|!~|=|!/, 'operator'],
        [/-?\d+(\.\d+)?([a-z]+)?\b/, 'number'],
        [/[\w.]+(?=:)/, 'variable'],
        [/[\w.\-*]+/, { cases: { '@keywords': 'keyword', '@default': 'identifier' } }],
        [/[,*]/, 'delimiter'],
      ],
      pipe: [[/[a-z_]+/, { cases: { '@pipes': { token: 'type', next: '@pop' }, '@default': { token: 'identifier', next: '@pop' } } }], [/./, '', '@pop']],
      dstring: [[/[^\\"]+/, 'string'], [/\\./, 'string.escape'], [/"/, 'string', '@pop']],
      sstring: [[/[^\\']+/, 'string'], [/\\./, 'string.escape'], [/'/, 'string', '@pop']],
      bstring: [[/[^`]+/, 'string'], [/`/, 'string', '@pop']],
    },
  })
  monaco.languages.registerCompletionItemProvider(languageId, {
    triggerCharacters: [':', '|', '{', ' ', '(', ',', '='],
    async provideCompletionItems(model, position) {
      const source = sources.get(model.uri.toString())
      if (!source) return { suggestions: [] }
      const before = model.getValueInRange({ startLineNumber: 1, startColumn: 1, endLineNumber: position.lineNumber, endColumn: position.column })
      const word = model.getWordUntilPosition(position)
      const range = new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn)
      const items = await suggestions(before, source)
      return {
        suggestions: items.map((item, i) => ({
          ...item,
          range: item.replaceFrom !== undefined ? new monaco.Range(position.lineNumber, position.column - item.replaceFrom, position.lineNumber, position.column) : range,
          sortText: String(i).padStart(4, '0'),
        })),
      }
    },
  })
}

type Item = Omit<monaco.languages.CompletionItem, 'range'> & { replaceFrom?: number } // characters before the cursor to replace

const Kind = monaco.languages.CompletionItemKind

const valueItems = (values: ValueHits[], partial: string): Item[] =>
  values.map((v) => ({
    label: { label: v.value || '""', description: String(v.hits) },
    kind: Kind.Value,
    insertText: JSON.stringify(v.value),
    replaceFrom: partial.length,
  }))

// What fits the text before the cursor
async function suggestions(before: string, source: CompletionSource): Promise<Item[]> {
  // A pipe name right after |
  const pipe = /\|\s*([a-z_]*)$/i.exec(before)
  if (pipe) return pipes.map((name) => ({ label: name, kind: Kind.Function, insertText: name + ' ' }))

  // Inside an unclosed {…}: stream fields, or a stream field's values after =
  const open = before.lastIndexOf('{')
  if (open > before.lastIndexOf('}')) {
    const value = /([\w.]+)\s*(?:=|!=)\s*("?[^",}]*)$/.exec(before.slice(open))
    if (value) return valueItems(await source.streamValues(value[1]), value[2])
    return streamFields.map((f) => ({ label: f, kind: Kind.Field, insertText: f + '=' }))
  }

  // A stats function after "stats … "
  if (/\|\s*stats\b[^|]*(?:\)\s*|stats\s+)$/i.test(before)) {
    return statsFunctions.map((f) => ({ label: f, kind: Kind.Function, insertText: `${f}(` }))
  }

  // A field's values after field: or field:=
  const filter = /("(?:[^"\\]|\\.)*"|[\w.]+):(=|~|)("?[^\s"()]*)$/.exec(before)
  if (filter) {
    const field = filter[1].startsWith('"') ? (JSON.parse(filter[1]) as string) : filter[1]
    if (field === '_time') return []
    return valueItems(await source.fieldValues(field), filter[3])
  }

  // Otherwise a field name, or an operator between filters
  const names = await source.fieldNames()
  const inPipe = /\|[^|]*$/.test(before)
  return [
    ...names.map((name) => ({ label: name, kind: Kind.Field, insertText: fieldName(name) + (inPipe ? '' : ':') })),
    ...(inPipe ? [] : ['AND', 'OR', 'NOT'].map((k) => ({ label: k, kind: Kind.Keyword, insertText: k + ' ' }))),
  ]
}

// Marks where the query stopped parsing: the word ending at the error's
// offset (in UTF-8 bytes), or the whole query when it is unknown
export function markError(model: monaco.editor.ITextModel, error?: QueryError) {
  if (!error) {
    monaco.editor.setModelMarkers(model, languageId, [])
    return
  }
  const text = model.getValue()
  let end = text.length
  if (error.offset >= 0) {
    const bytes = new TextEncoder().encode(text)
    end = new TextDecoder().decode(bytes.slice(0, error.offset)).length
  }
  let start = end
  while (start > 0 && !/\s/.test(text[start - 1])) start--
  if (start === end) start = Math.max(0, end - 1)
  if (error.offset < 0) start = 0
  const from = model.getPositionAt(start)
  const to = model.getPositionAt(Math.max(end, start + 1))
  monaco.editor.setModelMarkers(model, languageId, [
    {
      severity: monaco.MarkerSeverity.Error,
      message: error.message,
      startLineNumber: from.lineNumber,
      startColumn: from.column,
      endLineNumber: to.lineNumber,
      endColumn: to.column,
    },
  ])
}
