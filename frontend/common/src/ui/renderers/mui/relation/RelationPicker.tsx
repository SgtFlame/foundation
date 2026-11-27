// mui.relationPicker (forge 40b): search as you type over an entity type's
// records, single or multiple, with a Create "…" row. The search is a
// one-off read per keystroke, debounced (records.search when the type has
// searchable fields, else a prefix scan of its title field through
// records.find), because keystroke search is local and the server only
// answers the query. The chosen records are shown by title, each kept live
// through its own records.get subscription. record.fieldEditor uses it for
// relation fields (but those to user, the tenant's people).

import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Autocomplete, Box, Chip, TextField, Typography } from '@mui/material'
import { useQuery } from '../../../../live/useQuery.ts'
import { Records, type RecordData } from '../../../../services/Records.ts'
import type { RendererProps } from '../../../points.ts'
import { debounce, idsOf, matchesWhere, offerCreate, pick, queryFor, remove, str, valueOf } from './picker.ts'

interface EntityDescription {
  entity?: { titleField?: string; fields?: { id: string; searchable?: boolean; hidden?: boolean }[] }
}

export interface RelationPickerProps {
  entity: string
  value: unknown
  multiple?: boolean
  where?: Record<string, unknown>
  // records.search when true and the type has searchable fields (the
  // default); a title prefix scan when false
  searchable?: boolean
  create?: boolean
  display?: string[] // fields shown under each option's title
  label?: string
  placeholder?: string
  problem?: string
  disabled?: boolean
  onChange(value: string | string[] | null): void
  onCreate?(text: string): void
}

// An option row, or the Create "…" row
type Option = { id: string; title: string; record?: RecordData } | { create: string }

const isCreate = (o: Option): o is { create: string } => 'create' in o

function titleFrom(r: RecordData | undefined, titleField?: string): string {
  if (!r) return ''
  return str((titleField ? r[titleField] : undefined) ?? r._title ?? r._id)
}

// Reports one chosen record's title, live
function Watch({ entity, id, titleField, onTitle }: { entity: string; id: string; titleField?: string; onTitle(id: string, title: string): void }) {
  const { data } = useQuery<RecordData | null>(entity && id ? 'records.get' : undefined, { entity, id })
  const title = data ? titleFrom(data, titleField) : ''
  useEffect(() => {
    if (title) onTitle(id, title)
  }, [id, title, onTitle])
  return null
}

export function RelationPicker(props: RelationPickerProps) {
  const { entity, multiple = false, where, create = false, display = [], label, placeholder, problem, disabled } = props
  const { data: desc } = useQuery<EntityDescription>(entity ? 'entity.describe' : undefined, { id: entity, published: true })
  const titleField = desc?.entity?.titleField
  const searchable = props.searchable !== false && !!desc?.entity?.fields?.some((f) => f.searchable && !f.hidden)
  const ids = idsOf(props.value)
  const [titles, setTitles] = useState<Record<string, string>>({})
  const onTitle = useMemo(() => (id: string, title: string) => setTitles((t) => (t[id] === title ? t : { ...t, [id]: title })), [])
  const [text, setText] = useState('')
  const [open, setOpen] = useState(false)
  const [found, setFound] = useState<RecordData[]>([])
  const [loading, setLoading] = useState(false)
  const whereKey = JSON.stringify(where ?? {})

  const search = useMemo(() => {
    let asked = 0 // the latest search, so an older answer is dropped
    return debounce((typed: string) => {
      const q = queryFor(entity, typed, { searchable, titleField, where: JSON.parse(whereKey) })
      const n = ++asked
      setLoading(true)
      const run =
        q.kind === 'search'
          ? Records.search(q.request as never).then((r) => r.records.filter((rec) => matchesWhere(rec, JSON.parse(whereKey))).slice(0, 20))
          : Records.find(q.request as never).then((r) => r.records)
      run
        .then((records) => {
          if (n === asked) setFound(records)
        })
        .catch(() => {
          if (n === asked) setFound([])
        })
        .finally(() => {
          if (n === asked) setLoading(false)
        })
    }, 250)
  }, [entity, searchable, titleField, whereKey])
  useEffect(() => () => search.cancel(), [search])
  useEffect(() => {
    if (open && entity) search.call(text)
  }, [open, text, entity, search])

  // What was found also names the chosen records until their own reads
  // arrive
  const titleOf = (id: string) => titles[id] ?? titleFrom(found.find((r) => str(r._id) === id), titleField) ?? id
  const options: Option[] = found.map((r) => ({ id: str(r._id), title: titleFrom(r, titleField), record: r }))
  const offer = offerCreate(create, text, options.map((o) => (isCreate(o) ? '' : o.title)))
  if (offer) options.push({ create: offer })
  const chosen: Option[] = ids.map((id) => ({ id, title: titleOf(id) || id }))

  const change = (next: string[]) => props.onChange(valueOf(next, multiple))
  const subtitle = (r?: RecordData): ReactNode => {
    if (!r || display.length === 0) return null
    const parts = display.map((f) => str(r[f])).filter(Boolean)
    return parts.length ? <Typography variant="caption" color="text.secondary" component="div">{parts.join(' · ')}</Typography> : null
  }

  return (
    <>
      {ids.map((id) => <Watch key={id} entity={entity} id={id} titleField={titleField} onTitle={onTitle} />)}
      <Autocomplete<Option, boolean, false, false>
        size="small" fullWidth multiple={multiple} disabled={disabled} open={open} loading={loading}
        onOpen={() => setOpen(true)} onClose={() => setOpen(false)}
        options={options}
        filterOptions={(x) => x} // the server already searched
        value={(multiple ? chosen : (chosen[0] ?? null)) as never}
        inputValue={multiple || open ? text : (chosen[0] && !isCreate(chosen[0]) ? chosen[0].title : text)}
        onInputChange={(_, t, reason) => {
          if (reason === 'input' || reason === 'clear') setText(t)
          if (reason === 'reset') setText('')
        }}
        getOptionLabel={(o) => (isCreate(o) ? `Create "${o.create}"` : o.title)}
        isOptionEqualToValue={(a, b) => !isCreate(a) && !isCreate(b) && a.id === b.id}
        getOptionKey={(o) => (isCreate(o) ? `create:${o.create}` : o.id)}
        noOptionsText={text ? 'No matches' : 'Type to search'}
        onChange={(_, v, reason, details) => {
          const option = details?.option
          if (option && isCreate(option)) {
            props.onCreate?.(option.create)
            setText('')
            return
          }
          if (reason === 'removeOption' && option && !isCreate(option)) return change(remove(ids, option.id))
          if (reason === 'clear') return change([])
          if (Array.isArray(v)) return change(v.filter((o) => !isCreate(o)).map((o) => (o as { id: string }).id))
          if (v && !isCreate(v)) return change(pick(ids, v.id, false))
          change([])
        }}
        renderOption={(liProps, o) => {
          const { key, ...rest } = liProps as typeof liProps & { key: string }
          return (
            <Box component="li" key={key} {...rest}>
              {isCreate(o) ? (
                <Typography variant="body2" color="primary">Create “{o.create}”</Typography>
              ) : (
                <Box>
                  <Typography variant="body2">{o.title || o.id}</Typography>
                  {subtitle(o.record)}
                </Box>
              )}
            </Box>
          )
        }}
        renderValue={
          multiple
            ? (vals, getItemProps) =>
                (Array.isArray(vals) ? vals : [vals]).map((o, index) => {
                  const { key, ...item } = getItemProps({ index }) as ReturnType<typeof getItemProps> & { key: string }
                  return <Chip size="small" {...item} key={key} label={isCreate(o) ? o.create : o.title} />
                })
            : undefined
        }
        renderInput={(params) => <TextField {...params} label={label} placeholder={ids.length ? undefined : placeholder} error={!!problem} helperText={problem} />}
      />
    </>
  )
}

// The renderer: value bound to local state keeps the choice in the browser
// until it is sent; change sends {value}, create {text}
export default function MuiRelationPicker(p: RendererProps) {
  const server = p.props.value
  const [own, setOwn] = useState<unknown>(server)
  const [seen, setSeen] = useState(server)
  if (server !== seen) {
    setSeen(server)
    setOwn(server)
  }
  const bound = p.node.bind?.value !== undefined
  const value = bound ? server : own
  const where = p.props.where && typeof p.props.where === 'object' ? (p.props.where as Record<string, unknown>) : undefined
  return (
    <Box sx={(p.props.sx as object) ?? { minWidth: 220 }}>
      <RelationPicker
        entity={str(p.props.entity)} value={value} multiple={!!p.props.multiple} where={where}
        searchable={p.props.searchable as boolean | undefined} create={!!p.props.create}
        display={Array.isArray(p.props.display) ? p.props.display.map(str) : []}
        label={p.props.label as string | undefined} placeholder={p.props.placeholder as string | undefined}
        problem={p.props.helperText as string | undefined} disabled={!!p.props.disabled}
        onChange={(v) => {
          if (!p.setLocal('value', v)) setOwn(v)
          p.emit('change', { value: v })
        }}
        onCreate={(text) => p.emit('create', { text })}
      />
    </Box>
  )
}
