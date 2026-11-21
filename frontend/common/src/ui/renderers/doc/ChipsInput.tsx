// mui.chipsInput (forge 07l): values typed as chips, for addresses, tags
// and people. Suggestions are a prop the page binds to a query; the typed
// text goes out, debounced, as the text event, for a handler to keep in
// the state that query binds. Free values are checked against pattern.
// Enter, Tab, comma and semicolon make a chip; a pasted list is split;
// Backspace in an empty input removes the last chip.

import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { Box, Chip, FormControl, FormHelperText, List, ListItemButton, ListItemText, Paper, Popper, Typography } from '@mui/material'
import type { RendererProps } from '../../points.ts'

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

interface Suggestion {
  value: string
  label?: string
  secondary?: string
}

function suggestionsOf(v: unknown): Suggestion[] {
  if (!Array.isArray(v)) return []
  return v
    .map((s) => (typeof s === 'string' ? { value: s } : s && typeof s === 'object' ? { value: str((s as Suggestion).value), label: (s as Suggestion).label, secondary: (s as Suggestion).secondary } : null))
    .filter((s): s is Suggestion => !!s && s.value !== '')
}

// splitList splits a pasted or typed list on commas, semicolons and line
// breaks, keeping a quoted name's comma ("Doe, Jane" <jane@x>)
export function splitList(text: string): string[] {
  const out: string[] = []
  let cur = ''
  let quoted = false
  let angle = false
  for (const ch of text) {
    if (ch === '"') quoted = !quoted
    if (ch === '<') angle = true
    if (ch === '>') angle = false
    if (!quoted && !angle && (ch === ',' || ch === ';' || ch === '\n' || ch === '\r')) {
      if (cur.trim()) out.push(cur.trim())
      cur = ''
      continue
    }
    cur += ch
  }
  if (cur.trim()) out.push(cur.trim())
  return out
}

export function ChipsInput(p: RendererProps) {
  const server = useMemo(() => (Array.isArray(p.props.value) ? (p.props.value as unknown[]).map(str) : []), [p.props.value])
  const [values, setValues] = useState(server)
  useEffect(() => setValues(server), [server])
  const [text, setText] = useState('')
  const [active, setActive] = useState(0)
  const [problem, setProblem] = useState('')
  const [focused, setFocused] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const anchor = useRef<HTMLDivElement>(null)
  const suggestions = suggestionsOf(p.props.suggestions)
  const labels = (p.props.labels as Record<string, string> | undefined) ?? {}
  const free = p.props.free !== false
  const pattern = useMemo(() => {
    try {
      return p.props.pattern ? new RegExp(str(p.props.pattern), 'i') : undefined
    } catch {
      return undefined
    }
  }, [p.props.pattern])
  const disabled = !!p.props.disabled
  // Suggestions matching what is typed: a query bound to them may have
  // narrowed them already, and a fixed list is narrowed here
  const typed = text.trim().toLowerCase()
  const shown = typed
    ? suggestions.filter((s) => !values.includes(s.value) && (s.value.toLowerCase().includes(typed) || !!s.label?.toLowerCase().includes(typed) || !!s.secondary?.toLowerCase().includes(typed))).slice(0, 8)
    : []
  const emit = useRef(p.emit)
  emit.current = p.emit

  // text, debounced, for the suggestions query
  useEffect(() => {
    const t = setTimeout(() => emit.current('text', { text }), 200)
    return () => clearTimeout(t)
  }, [text])

  const labelOf = (v: string) => labels[v] ?? suggestions.find((s) => s.value === v)?.label ?? v

  function commit(next: string[]) {
    setValues(next)
    p.emit('change', { value: next })
  }

  // add takes typed items: a suggestion's value as it is, a free one when
  // it matches; refused ones stay in the input
  function add(items: string[]): boolean {
    const next = [...values]
    const refused: string[] = []
    for (const item of items) {
      const known = suggestions.find((s) => s.value.toLowerCase() === item.toLowerCase() || s.label?.toLowerCase() === item.toLowerCase())
      const value = known?.value ?? item
      if (!known && (!free || (pattern && !pattern.test(item)))) {
        refused.push(item)
        continue
      }
      if (!next.includes(value)) next.push(value)
    }
    if (next.length !== values.length) commit(next)
    if (refused.length) {
      setProblem(`Not valid: ${refused.join(', ')}`)
      p.emit('invalid', { text: refused.join(', ') })
      setText(refused.join(', '))
      return false
    }
    setProblem('')
    setText('')
    return true
  }

  function pick(s: Suggestion) {
    if (!values.includes(s.value)) commit([...values, s.value])
    setText('')
    setProblem('')
    input.current?.focus()
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown' && shown.length) {
      e.preventDefault()
      setActive((a) => (a + 1) % shown.length)
    } else if (e.key === 'ArrowUp' && shown.length) {
      e.preventDefault()
      setActive((a) => (a - 1 + shown.length) % shown.length)
    } else if ((e.key === 'Enter' || e.key === 'Tab') && shown[active] && text.trim()) {
      e.preventDefault()
      pick(shown[active])
    } else if ((e.key === 'Enter' || (e.key === 'Tab' && text.trim()) || e.key === ',' || e.key === ';') && text.trim()) {
      e.preventDefault()
      add(splitList(text))
    } else if (e.key === 'Backspace' && !text && values.length) {
      commit(values.slice(0, -1))
    } else if (e.key === 'Escape') {
      setText('')
    }
  }

  const label = str(p.props.label)
  return (
    <FormControl fullWidth error={!!problem} disabled={disabled} variant="outlined" size="small">
      <Box ref={anchor} onClick={() => input.current?.focus()}
        sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, alignItems: 'center', minHeight: 40, px: 1, py: 0.5, border: 1, borderRadius: 1, bgcolor: 'background.paper', color: 'text.primary',
          borderColor: problem ? 'error.main' : focused ? 'primary.main' : 'divider', cursor: 'text' }}>
        {label && <Typography component="span" variant="body2" color="text.secondary" sx={{ mr: 0.5 }}>{label}</Typography>}
        {values.map((v) => (
          <Chip key={v} size="small" label={labelOf(v)} title={v} disabled={disabled} onDelete={disabled ? undefined : () => commit(values.filter((x) => x !== v))} />
        ))}
        <Box component="input" ref={input} value={text} disabled={disabled} placeholder={values.length ? '' : str(p.props.placeholder)}
          aria-label={label || 'values'}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => { setText(e.target.value); setActive(0); setProblem('') }}
          onKeyDown={onKey}
          onFocus={() => setFocused(true)}
          onBlur={() => { setFocused(false); if (text.trim()) add(splitList(text)) }}
          onPaste={(e: React.ClipboardEvent<HTMLInputElement>) => {
            const pasted = e.clipboardData.getData('text')
            if (splitList(pasted).length > 1) {
              e.preventDefault()
              add(splitList(text + pasted))
            }
          }}
          sx={{ flex: 1, minWidth: 80, border: 0, outline: 0, font: 'inherit', bgcolor: 'transparent', color: 'text.primary', py: 0.5 }} />
      </Box>
      {problem && <FormHelperText>{problem}</FormHelperText>}
      <Popper open={focused && shown.length > 0} anchorEl={anchor.current} placement="bottom-start" style={{ zIndex: 1500, width: anchor.current?.clientWidth }}>
        <Paper elevation={4}>
          <List dense disablePadding>
            {shown.map((s, i) => (
              <ListItemButton key={s.value} selected={i === active} onMouseDown={(e) => { e.preventDefault(); pick(s) }}>
                <ListItemText primary={s.label ?? s.value} secondary={s.secondary ?? (s.label ? s.value : undefined)} />
              </ListItemButton>
            ))}
          </List>
        </Paper>
      </Popper>
    </FormControl>
  )
}

export default ChipsInput
