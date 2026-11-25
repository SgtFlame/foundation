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
import { VolumeObject } from '../editors/volume/VolumeObject.ts'
import { useVolumes, type VolumeItem } from '../workspace/volumes.ts'
import { kindChronicle, kindVolume } from '../workspace/context.ts'
import { useActiveKnowledge, type ActiveKnowledge } from '../workspace/contextStore.ts'
import { useChatFocus, type ChatFocus } from './chatFocus.ts'
import { ancestorsOf, inputOf, refreshChronicles, typeOf, useChronicles } from './chronicleStore.ts'

const chronicleLog = log.In('chronicle')

interface TreeProps {
  entry: Entry
  depth: number
  childrenOf: Map<string, Entry[]>
  shown: Set<string> // what the filter keeps
  focus?: ChatFocus
  active: ActiveKnowledge
  implied: Set<string> // parents of checked entries: checked through them
  onOpen: (entry: Entry) => void
}

// One entry and, when expanded, its children
function EntryNode({ entry, depth, childrenOf, shown, focus, active, implied, onOpen }: TreeProps) {
  const [open, setOpen] = useState(true)
  const kids = (childrenOf.get(entry.id) ?? []).filter((e) => shown.has(e.id))
  const checked = active.checked(kindChronicle, entry.id)
  const viaChild = !checked && implied.has(entry.id)
  const chosen = focus?.chosen.includes(entry.id)
  const changed = focus?.changed.includes(entry.id)
  return (
    <>
      <Stack direction="row" sx={{ alignItems: 'center', pl: depth * 1.5, pr: 0.5, '&:hover': { bgcolor: 'action.hover' } }}>
        <IconButton size="small" sx={{ visibility: kids.length ? 'visible' : 'hidden', p: 0.25 }} onClick={() => setOpen((o) => !o)} aria-label={open ? 'Collapse' : 'Expand'}>
          {open ? <ExpandMoreIcon fontSize="small" /> : <ChevronRightIcon fontSize="small" />}
        </IconButton>
        <Tooltip title={viaChild ? 'Included because a child is checked' : 'Always use in this workspace'} placement="right">
          <span>
            <Checkbox
              size="small"
              sx={{ p: 0.25 }}
              checked={checked || viaChild}
              disabled={!active.workspace || viaChild}
              onChange={(_, on) => active.setChecked(kindChronicle, entry.id, on)}
              slotProps={{ input: { 'aria-label': `Always use ${entry.title}` } }}
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
          <EntryNode key={k.id} entry={k} depth={depth + 1} childrenOf={childrenOf} shown={shown} focus={focus} active={active} implied={implied} onOpen={onOpen} />
        ))}
      </Collapse>
    </>
  )
}

// A row for an item the person can no longer read: listed, greyed, never searched
function GoneRow({ label }: { label: string }) {
  return (
    <Stack direction="row" sx={{ alignItems: 'center', pl: 3.5, pr: 0.5, color: 'text.disabled' }}>
      <Checkbox size="small" sx={{ p: 0.25 }} disabled checked={false} slotProps={{ input: { 'aria-label': label } }} />
      <Typography variant="body2" sx={{ fontStyle: 'italic', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {label}
      </Typography>
    </Stack>
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

// The Context side view (forge 16c, once the Chronicles view): the
// workspace's active knowledge. A Volumes group with a checkbox per Volume
// (checked: chat searches it here), then the memory entries as a tree per
// type with a checkbox per entry (checked: always used here; checking an
// entry includes its parents). The checks are the workspace's, so the view
// works with no chat open and every window follows them. The dot and ✎
// still show what the focused chat's last reply used and changed.
function ContextView({ registry }: WorkbenchContext) {
  const { loaded, entries, types, error } = useChronicles()
  const focus = useChatFocus()
  const active = useActiveKnowledge()
  const { volumes } = useVolumes()
  const [filter, setFilter] = useState('')
  const [onlyActive, setOnlyActive] = useState(false)
  const [failure, setFailure] = useState<string>()

  const session = useSession()
  const me = userIdOf(session)
  const tenant = session.account?.tenantId ?? '1'
  // Your entries make the tree; those others shared with you are listed apart
  const isMine = useCallback((e: Entry) => e.ownerId === me && e.tenantId === tenant, [me, tenant])
  const activeEntries = useMemo(() => entries.filter((e) => e.status === 'active' && isMine(e)), [entries, isMine])
  const sharedWithMe = useMemo(() => entries.filter((e) => e.status === 'active' && !isMine(e)), [entries, isMine])
  const proposed = useMemo(() => entries.filter((e) => e.status === 'proposed'), [entries])
  const checkedIds = useMemo(() => Object.entries(active.context[kindChronicle] ?? {}).filter(([, on]) => on).map(([id]) => id), [active.context])
  const implied = useMemo(() => new Set(checkedIds.flatMap((id) => ancestorsOf(activeEntries, id))), [checkedIds, activeEntries])
  const q = filter.trim().toLowerCase()

  // What the filter and the in-use toggle keep, with the parents of what they keep
  const shown = useMemo(() => {
    const inUse = new Set([...checkedIds, ...(focus?.chosen ?? []), ...implied])
    const keep = activeEntries.filter(
      (e) => (!q || e.title.toLowerCase().includes(q) || e.shortSummary.toLowerCase().includes(q)) && (!onlyActive || inUse.has(e.id)),
    )
    return new Set(keep.flatMap((e) => [e.id, ...ancestorsOf(activeEntries, e.id)]))
  }, [activeEntries, q, onlyActive, focus, implied, checkedIds])

  const { roots, children } = useMemo(() => {
    const ids = new Set(activeEntries.map((e) => e.id))
    const children = new Map<string, Entry[]>()
    const roots: Entry[] = []
    for (const e of activeEntries) {
      if (e.parentId && ids.has(e.parentId)) children.set(e.parentId, [...(children.get(e.parentId) ?? []), e])
      else roots.push(e)
    }
    return { roots, children }
  }, [activeEntries])

  // Volumes: the readable ones, then those the workspace names that the
  // person can no longer read, greyed
  const shownVolumes = volumes.filter((v) => (!q || v.name.toLowerCase().includes(q)) && (!onlyActive || active.checked(kindVolume, v.id)))
  const goneVolumes = Object.keys(active.context[kindVolume] ?? {}).filter((id) => !volumes.some((v) => v.id === id))
  const goneEntries = loaded ? checkedIds.filter((id) => !entries.some((e) => e.id === id)) : []

  const open = (entry: Entry) => registry.edit(new ChronicleObject(entry))
  const openVolume = (v: VolumeItem) => registry.edit(new VolumeObject(v.id, v.path.split('/').filter(Boolean)))
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
  const problem = failure ?? active.error ?? error

  return (
    <Stack spacing={0.5} sx={{ pb: 1 }}>
      <Stack direction="row" sx={{ px: 1, alignItems: 'center' }} spacing={0.5}>
        <TextField
          size="small"
          placeholder="Filter"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchIcon fontSize="small" /></InputAdornment> }, htmlInput: { 'aria-label': 'Filter the context' } }}
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
        {active.workspace
          ? `Active in “${active.workspace.name}”: chat searches the checked Volumes and always uses the checked entries.`
          : 'Loading your workspace…'}
      </Typography>
      <FormControlLabel
        sx={{ px: 1.5 }}
        control={<Switch size="small" checked={onlyActive} onChange={(_, on) => setOnlyActive(on)} />}
        label={<Typography variant="caption">Only what is in use</Typography>}
      />
      {problem && (
        <Alert severity="warning" sx={{ mx: 1 }}>
          {problem}
        </Alert>
      )}
      {(volumes.length > 0 || goneVolumes.length > 0) && (
        <Group title="📚 Volumes" count={volumes.length}>
          {shownVolumes.map((v) => (
            <Stack key={v.id} direction="row" sx={{ alignItems: 'center', pl: 2, pr: 0.5, '&:hover': { bgcolor: 'action.hover' } }}>
              <Tooltip title="Search in this workspace" placement="right">
                <Checkbox
                  size="small"
                  sx={{ p: 0.25 }}
                  checked={active.checked(kindVolume, v.id)}
                  disabled={!active.workspace}
                  onChange={(_, on) => active.setChecked(kindVolume, v.id, on)}
                  slotProps={{ input: { 'aria-label': `Search ${v.name}` } }}
                />
              </Tooltip>
              <Box component="button" onClick={() => openVolume(v)} title={`/${v.path}`}
                sx={{ flex: 1, minWidth: 0, textAlign: 'left', border: 0, bgcolor: 'transparent', color: 'text.primary', cursor: 'pointer', p: 0.25,
                  font: 'inherit', fontSize: 14, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              >
                {v.name}
              </Box>
            </Stack>
          ))}
          {!onlyActive && goneVolumes.map((id) => <GoneRow key={id} label="A Volume you can no longer read" />)}
        </Group>
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
              <EntryNode key={e.id} entry={e} depth={0} childrenOf={children} shown={shown} focus={focus} active={active} implied={implied} onOpen={open} />
            ))}
          </Group>
        )
      })}
      {sharedWithMe.length > 0 && (
        <Group title="🤝 Shared with you" count={sharedWithMe.length}>
          {sharedWithMe
            .filter((e) => !q || e.title.toLowerCase().includes(q))
            .map((e) => (
              <EntryNode key={e.id} entry={e} depth={0} childrenOf={noChildren} shown={new Set([e.id])} focus={focus} active={active} implied={implied} onOpen={open} />
            ))}
        </Group>
      )}
      {!onlyActive && goneEntries.length > 0 && (
        <Group title="No longer readable" count={goneEntries.length}>
          {goneEntries.map((id) => <GoneRow key={id} label="An entry you can no longer read" />)}
        </Group>
      )}
      {loaded && activeEntries.length === 0 && proposed.length === 0 && !error && (
        <Typography variant="body2" color="text.secondary" sx={{ px: 1.5, py: 1 }}>
          No Chronicles yet. Add one with +, or chat: after each reply, the memory update proposes entries worth remembering.
        </Typography>
      )}
    </Stack>
  )
}

export default ContextView
