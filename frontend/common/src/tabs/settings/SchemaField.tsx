// The generic settings form: renders any value from its schema
// (settings/schema.ts). Top-level leaves show where their value comes from
// and can be reset to the inherited value; fields inside lists cannot,
// because a list is inherited as a whole.

import { useState, type ReactNode } from 'react'
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Button,
  Chip,
  FormControlLabel,
  FormHelperText,
  IconButton,
  InputAdornment,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import ArrowDownwardIcon from '@mui/icons-material/ArrowDownward'
import ArrowUpwardIcon from '@mui/icons-material/ArrowUpward'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutlined'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import RestartAltIcon from '@mui/icons-material/RestartAlt'
import UndoIcon from '@mui/icons-material/Undo'
import type { Scope, ValueSource } from '../../services/Config.ts'
import { useConfig } from '../../settings/configStore.ts'
import { configuration } from '../../settings/points.ts'
import { allowsScope, emptyValue, getAt, isSecretView, join, schemaAt, type Schema, type Value } from '../../settings/schema.ts'
import { extensions } from '../../workbench/extensions/index.ts'

// What every field of one section form shares
export interface FieldContext {
  sectionId: string
  rootSchema: Schema
  sectionScopes: Scope[]
  scope: Scope // the scope being edited
  readOnly?: boolean // the user may not write this section's plain settings at this scope
  // the user may not write its secrets: at the app scope they need
  // secret.write (an Application Administrator), elsewhere config.writeSecrets
  secretsReadOnly?: boolean
  original: Value // the value editing started from
  draft: Value // the value being edited
  sources: Record<string, ValueSource>
  resets: ReadonlySet<string>
  toggleReset: (path: string) => void
  errors: Record<string, string> // by path
  onChange: (path: string, value: unknown) => void
}

interface FieldProps {
  schema: Schema
  path: string
  value: unknown
  ctx: FieldContext
  inList?: boolean // inside an array item: no source badge or reset
}

const sourceLabels: Record<ValueSource, string> = {
  default: 'default',
  server: 'server',
  env: 'environment',
  app: 'app',
  tenant: 'tenant',
  user: 'user',
}

export function SchemaField({ schema, path, value, ctx, inList }: FieldProps) {
  if (schema.type === 'object') return <ObjectField schema={schema} path={path} value={value} ctx={ctx} inList={inList} />
  const input =
    schema.type === 'array' ? (
      <ArrayField schema={schema} path={path} value={value} ctx={ctx} />
    ) : (
      <LeafInput schema={schema} path={path} value={value} ctx={ctx} />
    )
  return inList ? input : <LeafFrame schema={schema} path={path} ctx={ctx}>{input}</LeafFrame>
}

function ObjectField({ schema, path, value, ctx, inList }: FieldProps) {
  const children = Object.entries(schema.properties ?? {}).map(([k, child]) => (
    <SchemaField key={k} schema={child} path={join(path, k)} value={getAt(value, k)} ctx={ctx} inList={inList} />
  ))
  if (path === '' || (inList && !schema.title)) return <Stack spacing={1.5}>{children}</Stack>
  return (
    <Box>
      <Typography variant="subtitle2">{schema.title ?? path.split('.').pop()}</Typography>
      {schema.description && (
        <Typography variant="caption" sx={{ color: 'text.secondary' }}>
          {schema.description}
        </Typography>
      )}
      <Stack spacing={1.5} sx={{ mt: 1, pl: 1.5, borderLeft: 2, borderColor: 'divider' }}>
        {children}
      </Stack>
    </Box>
  )
}

// A top-level leaf: its input, where its value comes from, and reset
function LeafFrame({ schema, path, ctx, children }: { schema: Schema; path: string; ctx: FieldContext; children: ReactNode }) {
  const source = ctx.sources[path]
  const resetting = ctx.resets.has(path)
  const overriddenHere = source === ctx.scope
  const allowed = allowsScope(ctx.rootSchema, path, ctx.sectionScopes, ctx.scope)
  return (
    <Box sx={{ opacity: resetting ? 0.6 : 1 }}>
      {children}
      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', mt: 0.25, minHeight: 24 }}>
        {source && (
          <Tooltip title={`The value comes from the ${sourceLabels[source]} ${source === 'default' || source === 'env' || source === 'server' ? 'value' : 'scope'}`}>
            <Chip size="small" variant={overriddenHere ? 'filled' : 'outlined'} label={sourceLabels[source]} sx={{ height: 18, fontSize: 11 }} />
          </Tooltip>
        )}
        {!allowed && (
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>
            not set at the {ctx.scope} scope
          </Typography>
        )}
        {schema.restart && (
          <Typography variant="caption" sx={{ color: 'warning.main' }}>
            needs restart
          </Typography>
        )}
        {resetting && (
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>
            inherits on save
          </Typography>
        )}
        <Box sx={{ flex: 1 }} />
        {(overriddenHere || resetting) && (
          <Tooltip title={resetting ? 'Keep this value' : 'Reset to the inherited value'}>
            <IconButton size="small" onClick={() => ctx.toggleReset(path)} aria-label={resetting ? 'Keep this value' : 'Reset to the inherited value'}>
              {resetting ? <UndoIcon sx={{ fontSize: 16 }} /> : <RestartAltIcon sx={{ fontSize: 16 }} />}
            </IconButton>
          </Tooltip>
        )}
      </Stack>
    </Box>
  )
}

function LeafInput({ schema, path, value, ctx }: FieldProps) {
  const label = schema.title ?? path.split('.').pop()
  const error = ctx.errors[path]
  const helperText = error ?? schema.description
  const disabled = !!ctx.readOnly || !allowsScope(ctx.rootSchema, path, ctx.sectionScopes, ctx.scope)
  const set = (v: unknown) => ctx.onChange(path, v)
  const unit = schema.unit && {
    input: { endAdornment: <InputAdornment position="end">{schema.format === 'currency' ? `$ ${schema.unit}` : schema.unit}</InputAdornment> },
  }

  if (schema.ref) return <RefSelect schema={schema} path={path} value={value} ctx={ctx} />
  switch (schema.type) {
    case 'secret':
      return <SecretInput schema={schema} path={path} value={value} ctx={ctx} />
    case 'boolean':
      return (
        <Box>
          <FormControlLabel
            control={<Switch size="small" checked={value === true} onChange={(e) => set(e.target.checked)} disabled={disabled} />}
            label={<Typography variant="body2">{label}</Typography>}
          />
          {helperText && <FormHelperText error={!!error}>{helperText}</FormHelperText>}
        </Box>
      )
    case 'enum':
      return (
        <TextField select size="small" fullWidth label={label} value={typeof value === 'string' ? value : ''} onChange={(e) => set(e.target.value)} error={!!error} helperText={helperText} disabled={disabled}>
          {(schema.options ?? []).map((o) => (
            <MenuItem key={o.value} value={o.value}>
              {o.label ?? o.value}
            </MenuItem>
          ))}
        </TextField>
      )
    case 'number':
    case 'integer':
      return (
        <TextField
          size="small"
          fullWidth
          type="number"
          label={label}
          value={typeof value === 'number' ? value : ''}
          onChange={(e) => set(e.target.value === '' ? undefined : Number(e.target.value))}
          error={!!error}
          helperText={helperText}
          disabled={disabled}
          slotProps={{ ...unit, htmlInput: { min: schema.min, max: schema.max, step: schema.step ?? (schema.type === 'integer' ? 1 : 'any') } }}
        />
      )
    default:
      return (
        <TextField
          size="small"
          fullWidth
          label={label}
          value={typeof value === 'string' ? value : ''}
          onChange={(e) => set(e.target.value === '' ? undefined : e.target.value)}
          error={!!error}
          helperText={helperText}
          disabled={disabled}
          multiline={schema.format === 'multiline'}
          minRows={schema.format === 'multiline' ? 3 : undefined}
          slotProps={unit || undefined}
        />
      )
  }
}

// A secret is never shown: the field says whether one is set and takes a
// new value, which replaces it on save
function SecretInput({ schema, path, value, ctx }: FieldProps) {
  const label = schema.title ?? path.split('.').pop()
  const error = ctx.errors[path]
  const original = getAt(ctx.original, path)
  const stored = isSecretView(original) ? original : undefined
  if (ctx.sources[path] === 'env') {
    return (
      <TextField size="small" fullWidth disabled label={label} value="" placeholder="Set by environment"
        helperText="A flag or environment variable sets this, and wins over a stored value" />
    )
  }
  const replacing = typeof value === 'string'
  const clearing = value === null
  const managed = !!ctx.secretsReadOnly
  const locked = managed || !allowsScope(ctx.rootSchema, path, ctx.sectionScopes, ctx.scope)
  const status = clearing ? 'cleared on save' : replacing ? 'replaced on save' : stored?.set ? `set ${stored.hint ?? ''}`.trim() : 'not set'
  const help =
    error ??
    (managed && ctx.scope === 'app'
      ? 'Managed by an Application Administrator'
      : ctx.sources[path] === 'server'
        ? 'The server’s own key from Vault is used; a value stored here wins over it'
        : schema.description)
  return (
    <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
      <TextField
        size="small"
        fullWidth
        type="password"
        autoComplete="new-password"
        label={`${label} (${status})`}
        value={replacing ? value : ''}
        placeholder={stored?.set ? 'Enter a new value to replace it' : 'Enter a value'}
        onChange={(e) => ctx.onChange(path, e.target.value === '' ? original : e.target.value)}
        disabled={locked}
        error={!!error}
        helperText={help}
      />
      {stored?.set && !clearing && !locked && (
        <Tooltip title="Clear the stored value">
          <IconButton size="small" onClick={() => ctx.onChange(path, null)} aria-label="Clear the stored value" sx={{ mt: 0.5 }}>
            <DeleteOutlineIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      )}
      {clearing && (
        <Tooltip title="Keep the stored value">
          <IconButton size="small" onClick={() => ctx.onChange(path, original)} aria-label="Keep the stored value" sx={{ mt: 0.5 }}>
            <UndoIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      )}
    </Stack>
  )
}

// A string naming an item of another section's list, e.g. a level's model
function RefSelect({ schema, path, value, ctx }: FieldProps) {
  const [section, arrayPath = ''] = (schema.ref ?? '').split('#')
  const other = useConfig(section, ctx.scope).view?.value
  const items = getAt(section === ctx.sectionId ? ctx.draft : other, arrayPath)
  const target = section === ctx.sectionId ? ctx.rootSchema : (extensions.get(configuration).find((c) => c.id === section)?.schema as Schema | undefined)
  const keyName = schemaAt(target, arrayPath)?.key ?? 'id'
  const options = (Array.isArray(items) ? items : []).flatMap((item) => {
    const k = getAt(item, keyName)
    if (typeof k !== 'string') return []
    const title = getAt(item, 'title') ?? getAt(item, 'label')
    return [{ value: k, label: typeof title === 'string' && title !== k ? `${title} (${k})` : k }]
  })
  if (typeof value === 'string' && value !== '' && !options.some((o) => o.value === value)) {
    options.push({ value, label: `${value} (missing)` })
  }
  const error = ctx.errors[path]
  return (
    <TextField select size="small" fullWidth label={schema.title ?? path.split('.').pop()} value={typeof value === 'string' ? value : ''}
      onChange={(e) => ctx.onChange(path, e.target.value)} error={!!error} helperText={error ?? schema.description}>
      {options.map((o) => (
        <MenuItem key={o.value} value={o.value}>
          {o.label}
        </MenuItem>
      ))}
    </TextField>
  )
}

function ArrayField({ schema, path, value, ctx }: FieldProps) {
  const items = Array.isArray(value) ? value : []
  const locked = !!ctx.readOnly || !allowsScope(ctx.rootSchema, path, ctx.sectionScopes, ctx.scope)
  const itemSchema = schema.items ?? { type: 'string' }
  const keyName = schema.key ?? 'id'
  const [expanded, setExpanded] = useState<number | false>(false)
  const error = ctx.errors[path]
  const set = (next: unknown[]) => ctx.onChange(path, next)
  const move = (i: number, by: number) => {
    const next = [...items]
    ;[next[i], next[i + by]] = [next[i + by], next[i]]
    set(next)
    setExpanded(false)
  }
  const remove = (i: number) => {
    set(items.filter((_, j) => j !== i))
    setExpanded(false)
  }
  const add = () => {
    set([...items, emptyValue(itemSchema)])
    setExpanded(items.length)
  }

  return (
    <Box>
      <Stack direction="row" sx={{ alignItems: 'center' }}>
        <Typography variant="subtitle2" sx={{ flex: 1 }}>
          {schema.title ?? path.split('.').pop()}{' '}
          <Typography component="span" variant="caption" sx={{ color: 'text.secondary' }}>
            {items.length}
          </Typography>
        </Typography>
        <Button size="small" startIcon={<AddIcon />} onClick={add} disabled={locked}>
          Add
        </Button>
      </Stack>
      {schema.description && (
        <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block' }}>
          {schema.description}
        </Typography>
      )}
      {error && <FormHelperText error>{error}</FormHelperText>}
      <Box sx={{ mt: 0.5 }}>
        {items.map((item, i) => {
          const itemPath = join(path, i)
          const hasError = Object.keys(ctx.errors).some((p) => p === itemPath || p.startsWith(itemPath + '.'))
          const actions = (
            <Stack direction="row">
              <IconButton size="small" disabled={locked || i === 0} onClick={() => move(i, -1)} aria-label="Move up">
                <ArrowUpwardIcon sx={{ fontSize: 16 }} />
              </IconButton>
              <IconButton size="small" disabled={locked || i === items.length - 1} onClick={() => move(i, 1)} aria-label="Move down">
                <ArrowDownwardIcon sx={{ fontSize: 16 }} />
              </IconButton>
              <IconButton size="small" disabled={locked} onClick={() => remove(i)} aria-label="Remove">
                <DeleteOutlineIcon sx={{ fontSize: 16 }} />
              </IconButton>
            </Stack>
          )
          if (itemSchema.type !== 'object') {
            return (
              <Stack key={i} direction="row" spacing={0.5} sx={{ alignItems: 'flex-start', mb: 1 }}>
                <Box sx={{ flex: 1 }}>
                  <LeafInput schema={itemSchema} path={itemPath} value={item} ctx={ctx} />
                </Box>
                {actions}
              </Stack>
            )
          }
          // The actions sit over the summary rather than in it, because the
          // summary is itself a button
          return (
            <Box key={i} sx={{ position: 'relative' }}>
              <Accordion disableGutters variant="outlined" expanded={expanded === i} onChange={(_, open) => setExpanded(open ? i : false)}
                slotProps={{ transition: { unmountOnExit: true } }}>
                <AccordionSummary expandIcon={<ExpandMoreIcon />} sx={{ minHeight: 40, '& .MuiAccordionSummary-content': { my: 0.5, mr: 13 } }}>
                  <Typography variant="body2" sx={{ color: hasError ? 'error.main' : undefined }}>
                    {itemTitle(item, keyName, i)}
                  </Typography>
                </AccordionSummary>
                <AccordionDetails>
                  <SchemaField schema={itemSchema} path={itemPath} value={item} ctx={ctx} inList />
                </AccordionDetails>
              </Accordion>
              <Box sx={{ position: 'absolute', top: 6, right: 40 }}>{actions}</Box>
            </Box>
          )
        })}
      </Box>
    </Box>
  )
}

function itemTitle(item: unknown, keyName: string, i: number): string {
  const key = getAt(item, keyName)
  const icon = getAt(item, 'icon')
  const title = getAt(item, 'title') ?? getAt(item, 'label')
  const parts = [typeof icon === 'string' ? icon : '', typeof title === 'string' ? title : '', key !== undefined && key !== '' ? `(${String(key)})` : '']
  const text = parts.filter(Boolean).join(' ')
  return text || `Item ${i + 1}`
}
