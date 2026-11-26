// The Log Explorer's structured builder: a model of the LogsQL it can show,
// the query it generates, and a parser that reads such a query back.
// Builder → LogsQL always works; LogsQL → Builder works when the query uses
// only what the builder can show, and is undefined otherwise.

import { logLevels, type LogLevel } from '../../logging/index.ts'
import { fieldName } from './view.ts'

export type Operator = 'equals' | 'contains' | 'prefix' | 'regex' | 'empty' | 'exists' | 'gt' | 'lt' | 'range' | 'in'

export const operators: { id: Operator; label: string; values: 0 | 1 | 2 }[] = [
  { id: 'equals', label: 'equals', values: 1 },
  { id: 'contains', label: 'contains', values: 1 },
  { id: 'prefix', label: 'starts with', values: 1 },
  { id: 'regex', label: 'matches regex', values: 1 },
  { id: 'empty', label: 'is empty', values: 0 },
  { id: 'exists', label: 'exists', values: 0 },
  { id: 'gt', label: 'greater than', values: 1 },
  { id: 'lt', label: 'less than', values: 1 },
  { id: 'range', label: 'in range', values: 2 },
  { id: 'in', label: 'one of', values: 1 }, // comma-separated
]

export interface FilterRow {
  field: string
  op: Operator
  value: string
  value2: string // the range's upper bound
  not: boolean
}

export interface FilterGroup {
  join: 'and' | 'or'
  rows: FilterRow[]
}

export interface StreamFilter {
  field: string
  values: string[]
}

export interface Pipes {
  uniqBy: string[]
  stats: boolean // stats by (statsBy) count() hits
  statsBy: string[]
  sortBy: string
  sortDesc: boolean
  limit: string
  fields: string[]
}

export interface Builder {
  streams: StreamFilter[]
  levels: LogLevel[] // none or all: every level
  words: string // a phrase on _msg
  groups: FilterGroup[] // ANDed together
  pipes: Pipes
}

export const streamFields = ['app', 'source', 'instance', 'tenant.id'] as const

export const emptyPipes: Pipes = { uniqBy: [], stats: false, statsBy: [], sortBy: '', sortDesc: true, limit: '', fields: [] }

export const emptyBuilder: Builder = {
  streams: streamFields.map((field) => ({ field, values: [] })),
  levels: [],
  words: '',
  groups: [],
  pipes: emptyPipes,
}

export const newRow = (field = '', op: Operator = 'equals', value = ''): FilterRow => ({ field, op, value, value2: '', not: false })

const q = (s: string) => JSON.stringify(s)
const list = (value: string) =>
  value
    .split(',')
    .map((v) => v.trim())
    .filter(Boolean)

function complete(row: FilterRow): boolean {
  const arity = operators.find((o) => o.id === row.op)?.values ?? 1
  if (!row.field) return false
  if (arity === 2) return row.value !== '' && row.value2 !== ''
  if (row.op === 'in') return list(row.value).length > 0
  return arity === 0 || row.value !== ''
}

function rowQL(row: FilterRow): string {
  const f = fieldName(row.field)
  const filter = (() => {
    switch (row.op) {
      case 'equals': return `${f}:=${q(row.value)}`
      case 'contains': return `${f}:${q(row.value)}`
      case 'prefix': return `${f}:${q(row.value)}*`
      case 'regex': return `${f}:~${q(row.value)}`
      case 'empty': return `${f}:""`
      case 'exists': return `${f}:*`
      case 'gt': return `${f}:>${row.value}`
      case 'lt': return `${f}:<${row.value}`
      case 'range': return `${f}:range[${row.value}, ${row.value2}]`
      case 'in': return `${f}:in(${list(row.value).map(q).join(', ')})`
    }
  })()
  return row.not ? `NOT ${filter}` : filter
}

export function toLogsQL(b: Builder): string {
  const parts: string[] = []
  const streams = b.streams.filter((s) => s.values.length > 0)
  if (streams.length) {
    const terms = streams.map((s) =>
      s.values.length === 1 ? `${s.field}=${q(s.values[0])}` : `${s.field} in(${s.values.map(q).join(', ')})`,
    )
    parts.push(`{${terms.join(', ')}}`)
  }
  if (b.levels.length === 1) parts.push(`level:=${b.levels[0]}`)
  else if (b.levels.length > 1 && b.levels.length < logLevels.length) parts.push(`level:in(${b.levels.join(', ')})`)
  if (b.words.trim()) parts.push(q(b.words.trim()))
  for (const group of b.groups) {
    const rows = group.rows.filter(complete).map(rowQL)
    if (rows.length === 1 && group.rows.length === 1) parts.push(rows[0])
    else if (rows.length) parts.push(`(${rows.join(group.join === 'or' ? ' OR ' : ' AND ')})`)
  }
  const p = b.pipes
  let text = parts.join(' ') || '*'
  if (p.uniqBy.length) text += ` | uniq by (${p.uniqBy.map(fieldName).join(', ')})`
  if (p.stats) text += ` | stats ${p.statsBy.length ? `by (${p.statsBy.map(fieldName).join(', ')}) ` : ''}count() hits`
  if (p.sortBy) text += ` | sort by (${fieldName(p.sortBy)}${p.sortDesc ? ' desc' : ''})`
  if (/^\d+$/.test(p.limit)) text += ` | limit ${p.limit}`
  if (p.fields.length) text += ` | fields ${p.fields.map(fieldName).join(', ')}`
  return text
}

// Parsing: a tokenizer and a recursive descent over what toLogsQL writes

type Token = { kind: 'punct' | 'string' | 'word'; text: string; spaced: boolean }

const puncts = [':=', ':~', ':>', ':<', '{', '}', '(', ')', '[', ']', ',', '|', '*', ':', '=']

function tokenize(text: string): Token[] {
  const tokens: Token[] = []
  let i = 0
  while (i < text.length) {
    const start = i
    while (i < text.length && /\s/.test(text[i])) i++
    const spaced = i > start || i === 0
    if (i >= text.length) break
    if (text[i] === '"') {
      let j = i + 1
      while (j < text.length && text[j] !== '"') j += text[j] === '\\' ? 2 : 1
      if (j >= text.length) throw new Error('unterminated string')
      tokens.push({ kind: 'string', text: JSON.parse(text.slice(i, j + 1)) as string, spaced })
      i = j + 1
      continue
    }
    const punct = puncts.find((p) => text.startsWith(p, i))
    if (punct) {
      tokens.push({ kind: 'punct', text: punct, spaced })
      i += punct.length
      continue
    }
    const word = /^[^\s{}()[\],|:"*=~<>!']+/.exec(text.slice(i))
    if (!word) throw new Error(`unexpected ${text[i]}`)
    tokens.push({ kind: 'word', text: word[0], spaced })
    i += word[0].length
  }
  return tokens
}

class Parser {
  private pos = 0
  private readonly tokens: Token[]

  constructor(tokens: Token[]) {
    this.tokens = tokens
  }

  peek(offset = 0): Token | undefined {
    return this.tokens[this.pos + offset]
  }

  done() {
    return this.pos >= this.tokens.length
  }

  next(): Token {
    const t = this.tokens[this.pos++]
    if (!t) throw new Error('unexpected end')
    return t
  }

  is(text: string, offset = 0) {
    const t = this.peek(offset)
    return t !== undefined && t.kind !== 'string' && t.text === text
  }

  isWord(text: string) {
    return this.peek()?.kind === 'word' && this.peek()!.text.toLowerCase() === text
  }

  expect(text: string) {
    const t = this.next()
    if (t.kind === 'string' || t.text.toLowerCase() !== text) throw new Error(`expected ${text}`)
  }

  string(): string {
    const t = this.next()
    if (t.kind !== 'string') throw new Error('expected a string')
    return t.text
  }

  name(): string {
    const t = this.next()
    if (t.kind === 'punct') throw new Error('expected a name')
    return t.text
  }

  // (a, b, …) of names or strings
  names(close = ')'): string[] {
    const out = [this.name()]
    while (this.is(',')) {
      this.next()
      out.push(this.name())
    }
    this.expect(close)
    return out
  }
}

function parseRow(p: Parser): FilterRow {
  let not = false
  if (p.isWord('not')) {
    p.next()
    not = true
  }
  const field = p.name()
  // _time and stream filters have their own syntax, which the builder does not show
  if (field === '_time' || field.startsWith('_stream')) throw new Error(`a ${field} filter`)
  const op = p.next()
  const row = (o: Operator, value = '', value2 = ''): FilterRow => ({ field, op: o, value, value2, not })
  switch (op.text) {
    case ':=': {
      // toLogsQL writes levels bare (level:=error), and f:="" is "is empty"
      const value = p.name()
      return value === '' ? row('empty') : row('equals', value)
    }
    case ':~': return row('regex', p.string())
    case ':>': return row('gt', p.name())
    case ':<': return row('lt', p.name())
    case ':': {
      if (p.is('*')) {
        p.next()
        return row('exists')
      }
      if (p.peek()?.kind === 'string') {
        const value = p.string()
        if (p.is('*') && !p.peek()!.spaced) {
          p.next()
          return row('prefix', value)
        }
        return value === '' ? row('empty') : row('contains', value)
      }
      if (p.isWord('in') && p.is('(', 1)) {
        p.next()
        p.next()
        return row('in', p.names().join(', '))
      }
      if (p.isWord('range') && p.is('[', 1)) {
        p.next()
        p.next()
        const [a, b] = p.names(']')
        return row('range', a, b ?? '')
      }
      // A bare word, as people write level:error
      if (p.peek()?.kind === 'word' && !p.peek()!.spaced) return row('contains', p.name())
    }
  }
  throw new Error(`cannot read the filter on ${field}`)
}

function parseStreams(p: Parser, b: Builder) {
  p.expect('{')
  while (!p.is('}')) {
    const field = p.name()
    const stream = b.streams.find((s) => s.field === field)
    if (!stream || stream.values.length) throw new Error(`stream field ${field}`)
    if (p.is('=')) {
      p.next()
      stream.values = [p.string()]
    } else {
      p.expect('in')
      p.expect('(')
      stream.values = p.names()
    }
    if (p.is(',')) p.next()
  }
  p.expect('}')
}

function parsePipe(p: Parser, b: Builder, seen: Set<string>) {
  const name = p.name().toLowerCase()
  if (seen.has(name)) throw new Error(`a second ${name}`)
  seen.add(name)
  const pipes = b.pipes
  switch (name) {
    case 'uniq':
      p.expect('by')
      p.expect('(')
      pipes.uniqBy = p.names()
      return
    case 'stats':
      pipes.stats = true
      if (p.isWord('by')) {
        p.next()
        p.expect('(')
        pipes.statsBy = p.names()
      }
      p.expect('count')
      p.expect('(')
      p.expect(')')
      p.expect('hits')
      return
    case 'sort':
      p.expect('by')
      p.expect('(')
      pipes.sortBy = p.name()
      pipes.sortDesc = p.isWord('desc')
      if (pipes.sortDesc) p.next()
      p.expect(')')
      return
    case 'limit': {
      const n = p.name()
      if (!/^\d+$/.test(n)) throw new Error('limit')
      pipes.limit = n
      return
    }
    case 'fields':
      pipes.fields = [p.name()]
      while (p.is(',')) {
        p.next()
        pipes.fields.push(p.name())
      }
      return
  }
  throw new Error(`the ${name} pipe`)
}

export function fromLogsQL(text: string): Builder | undefined {
  try {
    const p = new Parser(tokenize(text))
    const b: Builder = {
      ...emptyBuilder,
      streams: emptyBuilder.streams.map((s) => ({ ...s, values: [] })),
      groups: [],
      pipes: { ...emptyPipes },
    }
    if (p.is('{')) parseStreams(p, b)
    while (!p.done() && !p.is('|')) {
      if (p.is('*')) {
        p.next()
        continue
      }
      if (p.peek()?.kind === 'string' && !p.is(':', 1) && !p.is(':=', 1) && !p.is(':~', 1)) {
        if (b.words) throw new Error('a second phrase')
        b.words = p.string()
        continue
      }
      if (p.is('(')) {
        p.next()
        const group: FilterGroup = { join: 'and', rows: [parseRow(p)] }
        let join: string | undefined
        while (!p.is(')')) {
          const word = p.name().toLowerCase()
          if ((word !== 'and' && word !== 'or') || (join && join !== word)) throw new Error('mixed joins')
          join = word
          group.rows.push(parseRow(p))
        }
        p.expect(')')
        group.join = join === 'or' ? 'or' : 'and'
        if (group.rows.length < 2) throw new Error('a group of one')
        b.groups.push(group)
        continue
      }
      const row = parseRow(p)
      const levels = row.op === 'in' ? row.value.split(', ') : [row.value]
      if (row.field === 'level' && !row.not && !b.levels.length && (row.op === 'equals' || row.op === 'in') &&
          levels.every((l) => logLevels.includes(l as LogLevel))) {
        b.levels = logLevels.filter((l) => levels.includes(l))
        continue
      }
      b.groups.push({ join: 'and', rows: [row] })
    }
    const seen = new Set<string>()
    while (p.is('|')) {
      p.next()
      parsePipe(p, b, seen)
    }
    return p.done() ? b : undefined
  } catch {
    return undefined
  }
}
