// The MUI family of server-described components (mui.*), one renderer per
// primitive of backend/features/ui/components. Inputs whose value is bound
// to local state keep typing in the browser; their change events still go
// to the server when the tree maps them.

import {
  Alert,
  AlertTitle,
  Autocomplete,
  Avatar,
  Badge,
  Box,
  Button,
  Card,
  CardActions,
  CardContent,
  CardHeader,
  Checkbox,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  Drawer,
  FormControl,
  FormControlLabel,
  Grid,
  IconButton,
  InputLabel,
  LinearProgress,
  CircularProgress,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Select,
  Stack,
  Switch,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TableSortLabel,
  Tabs,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useRef, useState } from 'react'
import MarkdownText from '../../../components/markdown/MarkdownText.tsx'
import type { RendererProps } from '../../points.ts'
import { ErrorBox } from '../../UiRenderer.tsx'
import { NamedIcon } from './icons.tsx'

type P = RendererProps
const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))
const sxOf = (p: P) => p.props.sx as object | undefined

export function MuiStack(p: P) {
  const { direction, spacing, alignItems, justifyContent, wrap } = p.props
  return (
    <Stack direction={(direction as 'row' | 'column') ?? 'column'} spacing={(spacing as number) ?? 1}
      sx={{ alignItems: alignItems as string, justifyContent: justifyContent as string, flexWrap: wrap ? 'wrap' : undefined, ...sxOf(p) }} useFlexGap={!!wrap}>
      {p.children}
    </Stack>
  )
}

export function MuiGrid(p: P) {
  return (
    <Grid container spacing={(p.props.spacing as number) ?? 2} columns={(p.props.columns as number) ?? 12} sx={sxOf(p)}>
      {(p.node.children ?? []).map((c) => (
        <Grid key={c.key} size={(c.props?.size as number) ?? 12}>
          {p.renderChild(c.key)}
        </Grid>
      ))}
    </Grid>
  )
}

export function MuiBox(p: P) {
  return <Box sx={sxOf(p)}>{p.children}</Box>
}

export function MuiCard(p: P) {
  const actions = p.slot('actions')
  return (
    <Card variant={(p.props.variant as 'elevation' | 'outlined') ?? 'elevation'} sx={{ minWidth: 0, ...sxOf(p) }}>
      {(p.props.title !== undefined || p.props.subtitle !== undefined) && (
        <CardHeader title={str(p.props.title)} subheader={p.props.subtitle ? str(p.props.subtitle) : undefined} slotProps={{ title: { variant: 'subtitle1' } }} sx={{ pb: 0 }} />
      )}
      <CardContent>{p.children}</CardContent>
      {actions.length > 0 && <CardActions>{actions}</CardActions>}
    </Card>
  )
}

export function MuiDivider(p: P) {
  const vertical = p.props.orientation === 'vertical'
  return <Divider orientation={vertical ? 'vertical' : 'horizontal'} flexItem={vertical} />
}

export function MuiText(p: P) {
  return (
    <Typography variant={(p.props.variant as 'body1') ?? 'body1'} color={p.props.color as string} sx={sxOf(p)}>
      {str(p.props.text)}
    </Typography>
  )
}

export function MuiHeading(p: P) {
  const level = Math.min(Math.max(Number(p.props.level ?? 2), 1), 6)
  return (
    <Typography variant={`h${level}` as 'h1'} component={`h${level}` as 'h1'} sx={{ fontSize: undefined, ...sxOf(p) }}>
      {str(p.props.text)}
    </Typography>
  )
}

export function MuiMarkdown(p: P) {
  return <MarkdownText text={str(p.props.text)} />
}

export function MuiIcon(p: P) {
  return <NamedIcon name={str(p.props.name)} fontSize={(p.props.fontSize as 'small') ?? 'medium'} sx={{ color: p.props.color as string }} />
}

export function MuiAvatar(p: P) {
  const name = str(p.props.name)
  return (
    <Avatar src={p.props.src as string | undefined} alt={name} sx={{ bgcolor: p.props.color as string }}>
      {name.slice(0, 1).toUpperCase()}
    </Avatar>
  )
}

export function MuiChip(p: P) {
  return (
    <Chip label={str(p.props.label)} color={(p.props.color as 'default') ?? 'default'} variant={(p.props.variant as 'filled') ?? 'filled'} size="small"
      onClick={p.handles('click') ? () => p.emit('click') : undefined} onDelete={p.handles('delete') ? () => p.emit('delete') : undefined} />
  )
}

export function MuiBadge(p: P) {
  return (
    <Badge badgeContent={p.props.content as string} color={(p.props.color as 'primary') ?? 'primary'} variant={p.props.dot ? 'dot' : 'standard'}>
      {p.children}
    </Badge>
  )
}

// An input's value: controlled by its binding when it has one, otherwise
// held here from the server's value
function useValue<T>(p: P, initial: T): [T, (v: T) => void] {
  const [own, setOwn] = useState<T>(initial)
  const bound = p.node.bind?.value !== undefined
  const value = bound ? ((p.props.value as T) ?? initial) : (p.props.value !== undefined ? (p.props.value as T) : own)
  return [
    value,
    (v: T) => {
      if (!p.setLocal('value', v)) setOwn(v)
      p.emit('change', { value: v })
    },
  ]
}

export function MuiTextField(p: P) {
  const [value, setValue] = useValue<string>(p, '')
  const multiline = !!p.props.multiline
  return (
    <TextField size="small" label={p.props.label as string} placeholder={p.props.placeholder as string} value={value ?? ''}
      multiline={multiline} minRows={multiline ? ((p.props.rows as number) ?? 3) : undefined}
      helperText={p.props.helperText as string} error={!!p.props.error} disabled={!!p.props.disabled}
      onChange={(e) => setValue(e.target.value)}
      onBlur={() => p.emit('blur', { value })}
      onKeyDown={(e) => {
        if (e.key === 'Enter' && (!multiline || e.ctrlKey || e.metaKey)) {
          e.preventDefault()
          p.emit('submit', { value })
        }
      }}
    />
  )
}

export function MuiNumber(p: P) {
  const [value, setValue] = useValue<number | ''>(p, '')
  return (
    <TextField size="small" type="number" label={p.props.label as string} value={value ?? ''} disabled={!!p.props.disabled}
      slotProps={{ htmlInput: { min: p.props.min, max: p.props.max, step: p.props.step } }}
      onChange={(e) => setValue(e.target.value === '' ? '' : Number(e.target.value))}
      onKeyDown={(e) => e.key === 'Enter' && p.emit('submit', { value })} />
  )
}

interface Option {
  value: string
  label?: string
}

export function MuiSelect(p: P) {
  const [value, setValue] = useValue<string>(p, '')
  const options = (p.props.options as Option[]) ?? []
  const id = `ui-${p.path.join('-')}`
  return (
    <FormControl size="small" sx={{ minWidth: 160 }} disabled={!!p.props.disabled}>
      <InputLabel id={id}>{str(p.props.label)}</InputLabel>
      <Select labelId={id} label={str(p.props.label)} value={value ?? ''} onChange={(e) => setValue(String(e.target.value))}>
        {options.map((o) => (
          <MenuItem key={o.value} value={o.value}>
            {o.label ?? o.value}
          </MenuItem>
        ))}
      </Select>
    </FormControl>
  )
}

function useChecked(p: P): [boolean, (v: boolean) => void] {
  const [own, setOwn] = useState(false)
  const checked = p.props.checked !== undefined ? !!p.props.checked : own
  return [
    checked,
    (v) => {
      if (!p.setLocal('checked', v)) setOwn(v)
      p.emit('change', { value: v })
    },
  ]
}

export function MuiCheckbox(p: P) {
  const [checked, set] = useChecked(p)
  return <FormControlLabel label={str(p.props.label)} disabled={!!p.props.disabled} control={<Checkbox checked={checked} onChange={(e) => set(e.target.checked)} />} />
}

export function MuiSwitch(p: P) {
  const [checked, set] = useChecked(p)
  return <FormControlLabel label={str(p.props.label)} disabled={!!p.props.disabled} control={<Switch checked={checked} onChange={(e) => set(e.target.checked)} />} />
}

export function MuiDate(p: P) {
  const [value, setValue] = useValue<string>(p, '')
  const type = p.props.mode === 'time' ? 'time' : p.props.mode === 'datetime' ? 'datetime-local' : 'date'
  return (
    <TextField size="small" type={type} label={p.props.label as string} value={value ?? ''} disabled={!!p.props.disabled}
      slotProps={{ inputLabel: { shrink: true } }} onChange={(e) => setValue(e.target.value)} />
  )
}

export function MuiAutocomplete(p: P) {
  const options = (p.props.options as Option[]) ?? []
  const multiple = !!p.props.multiple
  const [value, setValue] = useValue<string | string[] | null>(p, multiple ? [] : null)
  const byValue = new Map(options.map((o) => [o.value, o]))
  const toOption = (v: string) => byValue.get(v) ?? { value: v }
  return (
    <Autocomplete<Option, boolean, false, boolean>
      size="small" multiple={multiple} freeSolo={!!p.props.freeSolo} disabled={!!p.props.disabled} sx={{ minWidth: 220 }}
      options={options}
      getOptionLabel={(o: Option | string) => (typeof o === 'string' ? o : (o.label ?? o.value))}
      isOptionEqualToValue={(a: Option | string, b: Option | string) => (typeof a === 'string' ? a : a.value) === (typeof b === 'string' ? b : b.value)}
      value={(multiple ? ((value as string[]) ?? []).map(toOption) : value ? toOption(value as string) : null) as never}
      onChange={(_, v) => {
        const pick = (o: Option | string) => (typeof o === 'string' ? o : o.value)
        setValue(Array.isArray(v) ? v.map(pick) : v ? pick(v as Option | string) : null)
      }}
      onInputChange={(_, text, reason) => reason === 'input' && p.emit('input', { value: text })}
      renderInput={(params) => <TextField {...params} label={p.props.label as string} />}
    />
  )
}

export function MuiButton(p: P) {
  return (
    <Button variant={(p.props.variant as 'text') ?? 'text'} color={(p.props.color as 'primary') ?? 'primary'} disabled={!!p.props.disabled}
      startIcon={p.props.icon ? <NamedIcon name={str(p.props.icon)} /> : undefined} onClick={() => p.emit('click')}>
      {str(p.props.label)}
    </Button>
  )
}

export function MuiIconButton(p: P) {
  const title = str(p.props.title)
  return (
    <Tooltip title={title}>
      <span>
        <IconButton aria-label={title} size="small" color={(p.props.color as 'default') ?? 'default'} disabled={!!p.props.disabled} onClick={() => p.emit('click')}>
          <NamedIcon name={str(p.props.icon)} fontSize="small" />
        </IconButton>
      </span>
    </Tooltip>
  )
}

export function MuiMenu(p: P) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const items = (p.props.items as { id: string; label?: string; icon?: string }[]) ?? []
  const label = str(p.props.label)
  return (
    <>
      {label ? (
        <Button size="small" startIcon={p.props.icon ? <NamedIcon name={str(p.props.icon)} /> : undefined} onClick={(e) => setAnchor(e.currentTarget)}>
          {label}
        </Button>
      ) : (
        <IconButton size="small" aria-label="Menu" onClick={(e) => setAnchor(e.currentTarget)}>
          <NamedIcon name={str(p.props.icon) || 'more_vert'} fontSize="small" />
        </IconButton>
      )}
      <Menu anchorEl={anchor} open={!!anchor} onClose={() => setAnchor(null)}>
        {items.map((i) => (
          <MenuItem key={i.id} onClick={() => { setAnchor(null); p.emit('select', { id: i.id }) }}>
            {i.icon && (
              <ListItemIcon>
                <NamedIcon name={i.icon} fontSize="small" />
              </ListItemIcon>
            )}
            {i.label ?? i.id}
          </MenuItem>
        ))}
      </Menu>
    </>
  )
}

export function MuiList(p: P) {
  const items = (p.props.items as { id: string; primary?: string; secondary?: string; icon?: string }[]) ?? []
  return (
    <List dense={!!p.props.dense} disablePadding>
      {items.map((i) => (
        <ListItemButton key={i.id} selected={p.props.selected === i.id} onClick={() => p.emit('select', { id: i.id })} onDoubleClick={() => p.emit('open', { id: i.id })}>
          {i.icon && (
            <ListItemIcon>
              <NamedIcon name={i.icon} />
            </ListItemIcon>
          )}
          <ListItemText primary={i.primary ?? i.id} secondary={i.secondary} />
        </ListItemButton>
      ))}
    </List>
  )
}

interface Column {
  field: string
  title?: string
  width?: number
  sortable?: boolean
}

export function MuiTable(p: P) {
  const columns = (p.props.columns as Column[]) ?? []
  const rows = (p.props.rows as Record<string, unknown>[]) ?? []
  const rowKey = str(p.props.rowKey) || 'id'
  const sort = p.props.sort as { field: string; direction: 'asc' | 'desc' } | undefined
  const selection = (p.props.selection as string) ?? 'none'
  const selected = new Set(((p.props.selected as unknown[]) ?? []).map(String))
  const height = (p.props.height as number) ?? 360
  const total = (p.props.total as number) ?? rows.length
  const scroller = useRef<HTMLDivElement>(null)
  const virtual = useVirtualizer({ count: rows.length, getScrollElement: () => scroller.current, estimateSize: () => 33, overscan: 10 })
  const items = virtual.getVirtualItems()
  const last = items[items.length - 1]
  if (last && last.index >= rows.length - 1 && rows.length < total && p.handles('page')) {
    queueMicrotask(() => p.emit('page', { offset: rows.length }))
  }
  const select = (key: string) => {
    if (selection === 'none') return
    const next = selection === 'single' ? [key] : selected.has(key) ? [...selected].filter((k) => k !== key) : [...selected, key]
    p.emit('select', { selected: next })
  }
  const pad = items.length ? items[0].start : 0
  const after = items.length ? virtual.getTotalSize() - items[items.length - 1].end : 0
  return (
    <Box ref={scroller} sx={{ height, overflow: 'auto' }}>
      <Table size="small" stickyHeader>
        <TableHead>
          <TableRow>
            {columns.map((c) => (
              <TableCell key={c.field} sx={{ width: c.width }}>
                {c.sortable ? (
                  <TableSortLabel active={sort?.field === c.field} direction={sort?.field === c.field ? sort.direction : 'asc'}
                    onClick={() => p.emit('sort', { field: c.field, direction: sort?.field === c.field && sort.direction === 'asc' ? 'desc' : 'asc' })}>
                    {c.title ?? c.field}
                  </TableSortLabel>
                ) : (c.title ?? c.field)}
              </TableCell>
            ))}
          </TableRow>
        </TableHead>
        <TableBody>
          {pad > 0 && <TableRow style={{ height: pad }} />}
          {items.map((v) => {
            const row = rows[v.index]
            const key = str(row[rowKey] ?? v.index)
            return (
              <TableRow key={key} hover selected={selected.has(key)} onClick={() => select(key)} onDoubleClick={() => p.emit('open', { key })} sx={{ cursor: selection !== 'none' ? 'pointer' : undefined }}>
                {columns.map((c) => (
                  <TableCell key={c.field}>{str(row[c.field])}</TableCell>
                ))}
              </TableRow>
            )
          })}
          {after > 0 && <TableRow style={{ height: after }} />}
        </TableBody>
      </Table>
    </Box>
  )
}

export function MuiTabs(p: P) {
  const tabs = (p.props.tabs as { value: string; label?: string }[]) ?? []
  const [own, setOwn] = useState(tabs[0]?.value)
  const selected = (p.props.selected as string) ?? own
  return (
    <Box>
      <Tabs value={selected ?? false} onChange={(_, v: string) => { setOwn(v); p.emit('change', { value: v }) }}>
        {tabs.map((t) => (
          <Tab key={t.value} value={t.value} label={t.label ?? t.value} />
        ))}
      </Tabs>
      <Box sx={{ pt: 1 }}>{selected ? p.renderChild(selected) : null}</Box>
    </Box>
  )
}

export function MuiDialog(p: P) {
  const actions = p.slot('actions')
  return (
    <Dialog open={!!p.props.open} onClose={() => p.emit('close')} maxWidth={(p.props.maxWidth as 'sm') ?? 'sm'} fullWidth>
      {p.props.title !== undefined && <DialogTitle>{str(p.props.title)}</DialogTitle>}
      <DialogContent>{p.children}</DialogContent>
      {actions.length > 0 && <DialogActions>{actions}</DialogActions>}
    </Dialog>
  )
}

export function MuiDrawer(p: P) {
  return (
    <Drawer open={!!p.props.open} anchor={(p.props.anchor as 'right') ?? 'right'} onClose={() => p.emit('close')}>
      <Box sx={{ width: (p.props.width as number) ?? 360, p: 2 }}>{p.children}</Box>
    </Drawer>
  )
}

export function MuiProgress(p: P) {
  const value = p.props.value as number | undefined
  const variant = value === undefined ? 'indeterminate' : 'determinate'
  return p.props.variant === 'circular' ? <CircularProgress size={24} variant={variant} value={value} /> : <LinearProgress variant={variant} value={value} />
}

export function MuiAlert(p: P) {
  return (
    <Alert severity={(p.props.severity as 'info') ?? 'info'} onClose={p.handles('close') ? () => p.emit('close') : undefined}>
      {p.props.title !== undefined && <AlertTitle>{str(p.props.title)}</AlertTitle>}
      {str(p.props.text)}
    </Alert>
  )
}

export function MuiTooltip(p: P) {
  return (
    <Tooltip title={str(p.props.title)}>
      <span>{p.children}</span>
    </Tooltip>
  )
}

export function MuiEmpty(p: P) {
  return (
    <Stack spacing={1} sx={{ alignItems: 'center', p: 3, color: 'text.secondary', textAlign: 'center' }}>
      {p.props.icon !== undefined && <NamedIcon name={str(p.props.icon)} fontSize="large" />}
      <Typography variant="subtitle1">{str(p.props.title)}</Typography>
      {p.props.text !== undefined && <Typography variant="body2">{str(p.props.text)}</Typography>}
      {p.props.action !== undefined && (
        <Button variant="outlined" size="small" onClick={() => p.emit('action')}>
          {str(p.props.action)}
        </Button>
      )}
    </Stack>
  )
}

export function UiError(p: P) {
  return <ErrorBox messages={((p.props.messages as string[]) ?? []).map(String)} />
}
