import { useState } from 'react'
import {
  Autocomplete,
  Box,
  Button,
  Checkbox,
  Collapse,
  FormControlLabel,
  IconButton,
  MenuItem,
  Select,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutlineOutlined'
import ExpandLess from '@mui/icons-material/ExpandLess'
import ExpandMore from '@mui/icons-material/ExpandMore'
import { logLevels, type LogLevel } from '../../logging/index.ts'
import type { ValueHits } from '../../services/Logs.ts'
import { newRow, operators, type Builder, type FilterGroup, type FilterRow, type Operator, type Pipes } from './builder.ts'
import type { CompletionSource } from './logsql.ts'

const small = { '& .MuiInputBase-root': { fontSize: 12 }, '& .MuiInputBase-input': { py: '3px !important' } }

// A text field that suggests a field's values, with their counts
function ValueInput({ field, value, onChange, completion, placeholder = 'value' }: {
  field: string
  value: string
  onChange: (value: string) => void
  completion: CompletionSource
  placeholder?: string
}) {
  const [options, setOptions] = useState<ValueHits[]>([])
  return (
    <Autocomplete
      freeSolo
      size="small"
      options={options}
      getOptionLabel={(o) => (typeof o === 'string' ? o : o.value)}
      renderOption={(props, o) => (
        <li {...props} key={o.value}>
          <Box sx={{ flex: 1, fontSize: 12, fontFamily: 'monospace' }}>{o.value || '(empty)'}</Box>
          <Typography variant="caption" sx={{ color: 'text.secondary', ml: 1 }}>{o.hits}</Typography>
        </li>
      )}
      inputValue={value}
      onInputChange={(_, v) => onChange(v)}
      onOpen={() => {
        if (field) void completion.fieldValues(field).then(setOptions)
      }}
      sx={{ minWidth: 140, flex: 1, ...small }}
      renderInput={(params) => <TextField {...params} placeholder={placeholder} />}
    />
  )
}

function FieldInput({ value, onChange, names }: { value: string; onChange: (v: string) => void; names: ValueHits[] }) {
  return (
    <Autocomplete
      freeSolo
      size="small"
      options={names.map((n) => n.value)}
      inputValue={value}
      onInputChange={(_, v) => onChange(v)}
      sx={{ width: 170, ...small }}
      renderInput={(params) => <TextField {...params} placeholder="field" />}
    />
  )
}

function FieldsInput({ label, value, onChange, names }: { label: string; value: string[]; onChange: (v: string[]) => void; names: ValueHits[] }) {
  return (
    <Autocomplete
      multiple
      freeSolo
      size="small"
      options={names.map((n) => n.value)}
      value={value}
      onChange={(_, v) => onChange(v)}
      sx={{ minWidth: 200, flex: 1, ...small }}
      renderInput={(params) => <TextField {...params} label={label} />}
    />
  )
}

function RowEditor({ row, onChange, onRemove, names, completion }: {
  row: FilterRow
  onChange: (row: FilterRow) => void
  onRemove: () => void
  names: ValueHits[]
  completion: CompletionSource
}) {
  const arity = operators.find((o) => o.id === row.op)?.values ?? 1
  return (
    <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
      <ToggleButton
        value="not"
        size="small"
        selected={row.not}
        onChange={() => onChange({ ...row, not: !row.not })}
        sx={{ py: 0, px: 0.75, fontSize: 11 }}
      >
        NOT
      </ToggleButton>
      <FieldInput value={row.field} onChange={(field) => onChange({ ...row, field })} names={names} />
      <Select
        size="small"
        value={row.op}
        onChange={(e) => onChange({ ...row, op: e.target.value as Operator })}
        sx={{ fontSize: 12, '& .MuiSelect-select': { py: '3px' } }}
      >
        {operators.map((o) => (
          <MenuItem key={o.id} value={o.id} sx={{ fontSize: 12 }}>
            {o.label}
          </MenuItem>
        ))}
      </Select>
      {arity >= 1 && (
        <ValueInput
          field={row.field}
          value={row.value}
          onChange={(value) => onChange({ ...row, value })}
          completion={completion}
          placeholder={row.op === 'in' ? 'a, b, c' : arity === 2 ? 'from' : 'value'}
        />
      )}
      {arity === 2 && (
        <ValueInput field={row.field} value={row.value2} onChange={(value2) => onChange({ ...row, value2 })} completion={completion} placeholder="to" />
      )}
      <Tooltip title="Remove the filter">
        <IconButton size="small" onClick={onRemove} aria-label="Remove the filter">
          <DeleteOutlineIcon fontSize="small" />
        </IconButton>
      </Tooltip>
    </Stack>
  )
}

function GroupEditor({ group, onChange, onRemove, names, completion }: {
  group: FilterGroup
  onChange: (group: FilterGroup) => void
  onRemove: () => void
  names: ValueHits[]
  completion: CompletionSource
}) {
  const setRow = (i: number, row?: FilterRow) => {
    const rows = row ? group.rows.map((r, j) => (j === i ? row : r)) : group.rows.filter((_, j) => j !== i)
    if (rows.length === 0) onRemove()
    else onChange({ ...group, rows })
  }
  return (
    <Box sx={{ borderLeft: 3, borderColor: group.rows.length > 1 ? 'primary.light' : 'transparent', pl: 0.5 }}>
      <Stack spacing={0.5}>
        {group.rows.map((row, i) => (
          <Stack key={i} direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
            {i > 0 && (
              <ToggleButtonGroup
                exclusive
                size="small"
                value={group.join}
                onChange={(_, join: 'and' | 'or' | null) => join && onChange({ ...group, join })}
              >
                <ToggleButton value="and" sx={{ py: 0, fontSize: 10 }}>AND</ToggleButton>
                <ToggleButton value="or" sx={{ py: 0, fontSize: 10 }}>OR</ToggleButton>
              </ToggleButtonGroup>
            )}
            <RowEditor row={row} onChange={(r) => setRow(i, r)} onRemove={() => setRow(i)} names={names} completion={completion} />
          </Stack>
        ))}
        <Box>
          <Button size="small" startIcon={<AddIcon />} onClick={() => onChange({ ...group, rows: [...group.rows, newRow()] })} sx={{ fontSize: 11, py: 0 }}>
            {group.join === 'or' ? 'OR' : 'AND'} filter in this group
          </Button>
        </Box>
      </Stack>
    </Box>
  )
}

function PipesEditor({ pipes, onChange, names }: { pipes: Pipes; onChange: (pipes: Pipes) => void; names: ValueHits[] }) {
  return (
    <Stack spacing={1} sx={{ pt: 1 }}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <FormControlLabel
          control={<Checkbox size="small" checked={pipes.stats} onChange={(e) => onChange({ ...pipes, stats: e.target.checked })} />}
          label={<Typography variant="caption">stats count() by</Typography>}
        />
        <FieldsInput label="by fields" value={pipes.statsBy} onChange={(statsBy) => onChange({ ...pipes, statsBy })} names={names} />
        <FieldsInput label="uniq by" value={pipes.uniqBy} onChange={(uniqBy) => onChange({ ...pipes, uniqBy })} names={names} />
      </Stack>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <FieldInput value={pipes.sortBy} onChange={(sortBy) => onChange({ ...pipes, sortBy })} names={names} />
        <FormControlLabel
          control={<Checkbox size="small" checked={pipes.sortDesc} onChange={(e) => onChange({ ...pipes, sortDesc: e.target.checked })} />}
          label={<Typography variant="caption">sort descending</Typography>}
        />
        <TextField
          size="small"
          label="limit"
          value={pipes.limit}
          onChange={(e) => onChange({ ...pipes, limit: e.target.value.replace(/\D/g, '') })}
          sx={{ width: 90, ...small }}
        />
        <FieldsInput label="fields" value={pipes.fields} onChange={(fields) => onChange({ ...pipes, fields })} names={names} />
      </Stack>
    </Stack>
  )
}

interface BuilderPanelProps {
  builder: Builder
  onChange: (builder: Builder) => void
  names: ValueHits[] // field names for the current range
  streamValues: Record<string, ValueHits[]>
  completion: CompletionSource
}

// The query as streams, levels, words, filter groups and pipes
function BuilderPanel({ builder, onChange, names, streamValues, completion }: BuilderPanelProps) {
  const [pipesOpen, setPipesOpen] = useState(false)
  const set = (patch: Partial<Builder>) => onChange({ ...builder, ...patch })

  return (
    <Stack spacing={0.75} sx={{ p: 1 }}>
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', alignItems: 'center' }}>
        {builder.streams.map((stream, i) => (
          <Autocomplete
            key={stream.field}
            multiple
            freeSolo
            size="small"
            options={(streamValues[stream.field] ?? []).map((v) => v.value)}
            value={stream.values}
            onChange={(_, values) => set({ streams: builder.streams.map((s, j) => (j === i ? { ...s, values } : s)) })}
            sx={{ minWidth: 150, ...small }}
            renderInput={(params) => <TextField {...params} label={stream.field} />}
          />
        ))}
        <ToggleButtonGroup
          size="small"
          value={builder.levels}
          onChange={(_, levels: LogLevel[]) => set({ levels: logLevels.filter((l) => levels.includes(l)) })}
          aria-label="Levels"
        >
          {logLevels.map((level) => (
            <ToggleButton key={level} value={level} sx={{ py: 0.25, fontSize: 11, textTransform: 'none' }}>
              {level}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
        <TextField
          size="small"
          placeholder="Words or a phrase in the message"
          value={builder.words}
          onChange={(e) => set({ words: e.target.value })}
          sx={{ flex: 1, minWidth: 200, ...small }}
        />
      </Stack>
      {builder.groups.map((group, i) => (
        <GroupEditor
          key={i}
          group={group}
          names={names}
          completion={completion}
          onChange={(g) => set({ groups: builder.groups.map((x, j) => (j === i ? g : x)) })}
          onRemove={() => set({ groups: builder.groups.filter((_, j) => j !== i) })}
        />
      ))}
      <Stack direction="row" spacing={1}>
        <Button size="small" startIcon={<AddIcon />} onClick={() => set({ groups: [...builder.groups, { join: 'and', rows: [newRow()] }] })}>
          Filter
        </Button>
        <Button size="small" startIcon={<AddIcon />} onClick={() => set({ groups: [...builder.groups, { join: 'or', rows: [newRow(), newRow()] }] })}>
          OR group
        </Button>
        <Button
          size="small"
          endIcon={pipesOpen ? <ExpandLess /> : <ExpandMore />}
          onClick={() => setPipesOpen((open) => !open)}
        >
          Pipes
        </Button>
      </Stack>
      <Collapse in={pipesOpen}>
        <PipesEditor pipes={builder.pipes} onChange={(pipes) => set({ pipes })} names={names} />
      </Collapse>
    </Stack>
  )
}

export default BuilderPanel
