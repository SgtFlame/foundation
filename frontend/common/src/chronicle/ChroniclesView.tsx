import { useCallback, useMemo, useState } from 'react'
import {
  Alert,
  Box,
  Checkbox,
  Collapse,
  FormControlLabel,
  IconButton,
  InputAdornment,
  Stack,
  Switch,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import CheckIcon from '@mui/icons-material/Check'
import ChevronRightIcon from '@mui/icons-material/ChevronRight'
import CloseIcon from '@mui/icons-material/Close'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import FlagIcon from '@mui/icons-material/Flag'
import RefreshIcon from '@mui/icons-material/Refresh'
import SearchIcon from '@mui/icons-material/Search'
import { log } from '../logging/index.ts'
import { Chronicle, type Entry } from '../services/Chronicle.ts'
import type { WorkbenchContext } from '../workbench/points.ts'
import { userIdOf } from '../session/permissions.ts'
import { useSession } from '../session/session.ts'
import { ChronicleObject } from '../editors/chronicle/ChronicleObject.ts'
import { useChatFocus, type ChatFocus } from './chatFocus.ts'
import { ancestorsOf, inputOf, refreshChronicles, typeOf, useChronicles } from './chronicleStore.ts'

const chronicleLog = log.In('chronicle')

interface TreeProps {
  entry: Entry
  depth: number
  childrenOf: Map<string, Entry[]>
  shown: Set<string> // what the filter keeps
  focus?: ChatFocus
  implied: Set<string> // parents of checked entries: checked through them
  onOpen: (entry: Entry) => void
  onToggle: (entry: Entry, checked: boolean) => void
}

// One entry and, when expanded, its children
function EntryNode({ entry, depth, childrenOf, shown, focus, implied, onOpen, onToggle }: TreeProps) {
  const [open, setOpen] = useState(true)
  const kids = (childrenOf.get(entry.id) ?? []).filter((e) => shown.has(e.id))
  const checked = focus?.checked.includes(entry.id) ?? false
  const viaChild = !checked && implied.has(entry.id)
  const chosen = focus?.chosen.includes(entry.id)
  const changed = focus?.changed.includes(entry.id)
  return (
    <>
      <Stack direction="row" sx={{ alignItems: 'center', pl: depth * 1.5, pr: 0.5, '&:hover': { bgcolor: 'action.hover' } }}>
        <IconButton size="small" sx={{ visibility: kids.length ? 'visible' : 'hidden', p: 0.25 }} onClick={() => setOpen((o) => !o)} aria-label={open ? 'Collapse' : 'Expand'}>
          {open ? <ExpandMoreIcon fontSize="small" /> : <ChevronRightIcon fontSize="small" />}
        </IconButton>
        <Tooltip
          title={!focus ? 'Open Chat to use entries in a conversation' : viaChild ? 'Included because a child is checked' : 'Use in the conversation'}
          placement="right"
        >
          <span>
            <Checkbox
              size="small"
              sx={{ p: 0.25 }}
              checked={checked || viaChild}
              disabled={!focus || viaChild}
              onChange={(_, on) => onToggle(entry, on)}
              slotProps={{ input: { 'aria-label': `Use ${entry.title} in the conversation` } }}
            />
          </span>
        </Tooltip>
        <Box
          component="button"
          onClick={() => onOpen(entry)}
          title={entry.shortSummary || entry.title}
          sx={{
            flex: 1, minWidth: 0, textAlign: 'left', border: 0, bgcolor: 'transparent', color: 'text.primary', cursor: 'pointer', p: 0.25,
            font: 'inherit', fontSize: 14, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          }}
        >
          {entry.title}
        </Box>
        {changed && (
          <Tooltip title="Changed by the last memory update">
            <Box component="span" aria-label="Changed by the last memory update" sx={{ fontSize: 12, color: 'secondary.main', px: 0.25 }}>
              ✎
            </Box>
          </Tooltip>
        )}
        {chosen && (
          <Tooltip title="Used in the last reply">
            <Box component="span" aria-label="Used in the last reply" sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: 'primary.main', mx: 0.5, flexShrink: 0 }} />
          </Tooltip>
        )}
        {entry.review && (
          <Tooltip title="Flagged for review: a conversation round that changed it was ignored">
            <FlagIcon fontSize="small" color="warning" aria-label="Flagged for review" />
          </Tooltip>
        )}
      </Stack>
      <Collapse in={open} unmountOnExit>
        {kids.map((k) => (
          <EntryNode key={k.id} entry={k} depth={depth + 1} childrenOf={childrenOf} shown={shown} focus={focus} implied={implied} onOpen={onOpen} onToggle={onToggle} />
        ))}
      </Collapse>
    </>
  )
}

function Group({ title, count, children }: { title: string; count: number; children: React.ReactNode }) {
  const [open, setOpen] = useState(true)
  return (
    <Box>
      <Stack direction="row" component="button" onClick={() => setOpen((o) => !o)} aria-expanded={open}
        sx={{ width: '100%', alignItems: 'center', border: 0, bgcolor: 'transparent', cursor: 'pointer', px: 0.5, py: 0.25, color: 'text.secondary', font: 'inherit' }}
      >
        {open ? <ExpandMoreIcon fontSize="small" /> : <ChevronRightIcon fontSize="small" />}
        <Typography variant="caption" sx={{ fontWeight: 600 }}>
          {title} ({count})
        </Typography>
      </Stack>
      <Collapse in={open}>{children}</Collapse>
    </Box>
  )
}

const noChildren = new Map<string, Entry[]>()

// The Chronicles side view: the principal's memory entries as a tree per
// type, with a checkbox per entry for the focused chat's conversation
// (checking an entry includes its parents), the entries the memory update
// proposed, to accept or dismiss, and a filter
function ChroniclesView({ registry }: WorkbenchContext) {
  const { loaded, entries, types, error } = useChronicles()
  const focus = useChatFocus()
  const [filter, setFilter] = useState('')
  const [onlyActive, setOnlyActive] = useState(false)
  const [failure, setFailure] = useState<string>()

  const session = useSession()
  const me = userIdOf(session)
  const tenant = session.account?.tenantId ?? '1'
  // Your entries make the tree; those others shared with you are listed apart
  const isMine = useCallback((e: Entry) => e.ownerId === me && e.tenantId === tenant, [me, tenant])
  const active = useMemo(() => entries.filter((e) => e.status === 'active' && isMine(e)), [entries, isMine])
  const sharedWithMe = useMemo(() => entries.filter((e) => e.status === 'active' && !isMine(e)), [entries, isMine])
  const proposed = useMemo(() => entries.filter((e) => e.status === 'proposed'), [entries])
  const implied = useMemo(() => new Set((focus?.checked ?? []).flatMap((id) => ancestorsOf(active, id))), [focus?.checked, active])

  // What the filter and the dormant toggle keep, with the parents of what they keep
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase()
    const inUse = new Set([...(focus?.checked ?? []), ...(focus?.chosen ?? []), ...implied])
    const keep = active.filter(
      (e) => (!q || e.title.toLowerCase().includes(q) || e.shortSummary.toLowerCase().includes(q)) && (!onlyActive || inUse.has(e.id)),
    )
    return new Set(keep.flatMap((e) => [e.id, ...ancestorsOf(active, e.id)]))
  }, [active, filter, onlyActive, focus, implied])

  const { roots, children } = useMemo(() => {
    const ids = new Set(active.map((e) => e.id))
    const children = new Map<string, Entry[]>()
    const roots: Entry[] = []
    for (const e of active) {
      if (e.parentId && ids.has(e.parentId)) children.set(e.parentId, [...(children.get(e.parentId) ?? []), e])
      else roots.push(e)
    }
    return { roots, children }
  }, [active])

  const open = (entry: Entry) => registry.edit(new ChronicleObject(entry))
  const toggle = (entry: Entry, on: boolean) => {
    if (!focus) return
    focus.setChecked(on ? [...focus.checked, entry.id] : focus.checked.filter((id) => id !== entry.id))
  }
  const run = async (what: string, action: () => Promise<unknown>) => {
    setFailure(undefined)
    try {
      await action()
    } catch (err) {
      chronicleLog.fromError(err).warn('cannot %s', what)
      setFailure(err instanceof Error ? err.message : String(err))
    }
    await refreshChronicles()
  }
  // The list has no full summaries, so the entry is read in full before it is saved
  const accept = (e: Entry) =>
    run('accept a proposed entry', async () => {
      const { entry } = await Chronicle.get(e.id)
      if (entry) await Chronicle.update({ ...inputOf(entry), id: entry.id, accept: true })
    })
  const dismiss = (e: Entry) => run('dismiss a proposed entry', () => Chronicle.delete(e.id))

  return (
    <Stack spacing={0.5} sx={{ pb: 1 }}>
      <Stack direction="row" sx={{ px: 1, alignItems: 'center' }} spacing={0.5}>
        <TextField
          size="small"
          placeholder="Filter"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchIcon fontSize="small" /></InputAdornment> }, htmlInput: { 'aria-label': 'Filter Chronicles' } }}
          sx={{ flex: 1 }}
        />
        <Tooltip title="New entry">
          <IconButton size="small" aria-label="New entry" onClick={() => registry.edit(new ChronicleObject())}>
            <AddIcon fontSize="small" />
          </IconButton>
        </Tooltip>
        <Tooltip title="Refresh">
          <IconButton size="small" aria-label="Refresh" onClick={() => void refreshChronicles()}>
            <RefreshIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Stack>
      <Typography variant="caption" color="text.secondary" sx={{ px: 1.5 }}>
        {focus
          ? `Checks apply to ${focus.title ? `“${focus.title}”` : 'the current conversation'}.`
          : 'Open Chat to choose entries for a conversation.'}
      </Typography>
      <FormControlLabel
        sx={{ px: 1.5 }}
        control={<Switch size="small" checked={onlyActive} onChange={(_, on) => setOnlyActive(on)} />}
        label={<Typography variant="caption">Only entries in use</Typography>}
      />
      {(error || failure) && (
        <Alert severity="warning" sx={{ mx: 1 }}>
          {failure ?? error}
        </Alert>
      )}
      {proposed.length > 0 && (
        <Group title="Proposed" count={proposed.length}>
          {proposed.map((e) => (
            <Stack key={e.id} direction="row" sx={{ alignItems: 'center', pl: 2, pr: 0.5, '&:hover': { bgcolor: 'action.hover' } }}>
              <Box component="span" sx={{ pr: 0.5 }}>{typeOf(types, e.type)?.icon}</Box>
              <Box component="button" onClick={() => open(e)} title={e.shortSummary}
                sx={{ flex: 1, minWidth: 0, textAlign: 'left', border: 0, bgcolor: 'transparent', cursor: 'pointer', font: 'inherit', fontSize: 14, fontStyle: 'italic',
                  overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'text.primary' }}
              >
                {e.title}
              </Box>
              <Tooltip title="Accept">
                <IconButton size="small" aria-label={`Accept ${e.title}`} onClick={() => void accept(e)}>
                  <CheckIcon fontSize="small" color="success" />
                </IconButton>
              </Tooltip>
              <Tooltip title="Dismiss">
                <IconButton size="small" aria-label={`Dismiss ${e.title}`} onClick={() => void dismiss(e)}>
                  <CloseIcon fontSize="small" />
                </IconButton>
              </Tooltip>
            </Stack>
          ))}
        </Group>
      )}
      {types.map((t) => {
        const own = roots.filter((e) => e.type === t.id && shown.has(e.id))
        if (own.length === 0) return null
        return (
          <Group key={t.id} title={`${t.icon} ${t.title}`} count={own.length}>
            {own.map((e) => (
              <EntryNode key={e.id} entry={e} depth={0} childrenOf={children} shown={shown} focus={focus} implied={implied} onOpen={open} onToggle={toggle} />
            ))}
          </Group>
        )
      })}
      {sharedWithMe.length > 0 && (
        <Group title="🤝 Shared with you" count={sharedWithMe.length}>
          {sharedWithMe
            .filter((e) => !filter.trim() || e.title.toLowerCase().includes(filter.trim().toLowerCase()))
            .map((e) => (
              <EntryNode key={e.id} entry={e} depth={0} childrenOf={noChildren} shown={new Set([e.id])} focus={focus} implied={implied} onOpen={open} onToggle={toggle} />
            ))}
        </Group>
      )}
      {loaded && active.length === 0 && proposed.length === 0 && !error && (
        <Typography variant="body2" color="text.secondary" sx={{ px: 1.5, py: 1 }}>
          No Chronicles yet. Add one with +, or chat: after each reply, the memory update proposes entries worth remembering.
        </Typography>
      )}
    </Stack>
  )
}

export default ChroniclesView
