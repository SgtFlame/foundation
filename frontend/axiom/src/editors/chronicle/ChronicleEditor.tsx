import { useEffect, useMemo, useState } from 'react'
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Alert,
  Autocomplete,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from '@mui/material'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import { ancestorsOf, inputOf, refreshChronicles, typeOf, useChronicles } from '../../chronicle/chronicleStore.ts'
import ShareChronicleDialog from '../../chronicle/ShareChronicleDialog.tsx'
import { userIdOf } from '../../session/permissions.ts'
import { useSession } from '../../session/session.ts'
import { log } from '../../logging/index.ts'
import { Chronicle, type Entry, type EntryInput, type Link, type LinkedRound } from '../../services/Chronicle.ts'
import { useWorkbenchSubject } from '../../workbench/context/WorkbenchContext.ts'
import type { EditorProps } from '../../workbench/points.ts'
import { ChronicleObject } from './ChronicleObject.ts'

const chronicleLog = log.In('chronicle')

// The schema of an entry type's fields: the settings schema language, of
// which the built-in types use enum, string and number
interface FieldSchema {
  type: string
  title?: string
  description?: string
  options?: { value: string; label?: string }[]
}

function TypeFields({ schema, value, onChange }: { schema: unknown; value: Record<string, unknown>; onChange: (v: Record<string, unknown>) => void }) {
  const properties = Object.entries((schema as { properties?: Record<string, FieldSchema> } | undefined)?.properties ?? {})
  if (properties.length === 0) return null
  return (
    <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', rowGap: 1 }}>
      {properties.map(([key, f]) => {
        const set = (v: unknown) => {
          const next = { ...value }
          if (v === '' || v === undefined) delete next[key]
          else next[key] = v
          onChange(next)
        }
        const common = { size: 'small' as const, label: f.title ?? key, helperText: f.description, sx: { minWidth: 200 } }
        if (f.type === 'enum') {
          return (
            <TextField key={key} {...common} select value={(value[key] as string) ?? ''} onChange={(e) => set(e.target.value)}>
              <MenuItem value="">
                <em>None</em>
              </MenuItem>
              {f.options?.map((o) => (
                <MenuItem key={o.value} value={o.value}>
                  {o.label ?? o.value}
                </MenuItem>
              ))}
            </TextField>
          )
        }
        const numeric = f.type === 'number' || f.type === 'integer'
        return (
          <TextField
            key={key}
            {...common}
            type={numeric ? 'number' : 'text'}
            value={(value[key] as string | number | undefined) ?? ''}
            onChange={(e) => set(numeric ? (e.target.value === '' ? undefined : Number(e.target.value)) : e.target.value)}
          />
        )
      })}
    </Stack>
  )
}

function RoundList({ rounds, onRestore }: { rounds: readonly LinkedRound[]; onRestore: (r: LinkedRound) => void }) {
  if (rounds.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">
        No conversation has changed this entry.
      </Typography>
    )
  }
  return (
    <Stack spacing={1}>
      {rounds.map((r) => (
        <Box key={`${r.roundId}-${r.action}-${r.time}`} sx={{ pl: 1, borderLeft: 2, borderColor: 'divider' }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'baseline' }}>
            <Typography variant="caption" sx={{ fontWeight: 600 }}>
              {r.action === 'created' ? 'Proposed' : 'Updated'}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {new Date(r.time).toLocaleString()} · {r.conversationTitle || 'Untitled conversation'}
            </Typography>
            {r.previous && (
              <Button size="small" onClick={() => onRestore(r)} title="Puts the summaries back as they were before this update; save to keep them">
                Restore earlier summaries
              </Button>
            )}
          </Stack>
          <Typography variant="body2" color="text.secondary" sx={{ whiteSpace: 'pre-wrap' }}>
            {r.text}
          </Typography>
        </Box>
      ))}
    </Stack>
  )
}

// Edits one Chronicle entry (documentation/plans/Chronicle.md): its title,
// type and parent, the short summary context matching reads, the full
// summary the reply reads, the full context, the type's fields, related
// entries, and the conversation rounds that made or changed it
function ChronicleEditor({ object, registry, tabId }: EditorProps<ChronicleObject>) {
  const { entries, types } = useChronicles()
  const [entry, setEntry] = useState<Entry>()
  const [form, setForm] = useState<EntryInput>(() => ({
    type: object.draft?.type ?? 'note', title: '', shortSummary: '', fullSummary: '', fullContext: '', fields: {}, parentId: object.draft?.parentId,
  }))
  const [links, setLinks] = useState<Link[]>([])
  const [rounds, setRounds] = useState<LinkedRound[]>([])
  const [loading, setLoading] = useState(!!object.entryId)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [sharing, setSharing] = useState(false)
  const session = useSession()
  // An entry shared with you is read-only: only its owner changes it
  const mine = !entry || (entry.ownerId === userIdOf(session) && entry.tenantId === (session.account?.tenantId ?? '1'))

  useEffect(() => {
    if (!object.entryId) return
    let cancelled = false
    Chronicle.get(object.entryId)
      .then((resp) => {
        if (cancelled || !resp.entry) return
        setEntry(resp.entry)
        setForm(inputOf(resp.entry))
        setLinks(resp.links ?? [])
        setRounds(resp.rounds ?? [])
      })
      .catch((err: unknown) => {
        chronicleLog.fromError(err).With('entryId', object.entryId).warn('cannot open the entry')
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [object.entryId])

  const saved = entry ? inputOf(entry) : undefined
  const dirty = JSON.stringify(form) !== JSON.stringify(saved)
  useWorkbenchSubject(
    tabId,
    { type: 'chronicle', id: entry?.id ?? object.id, label: form.title || 'New Chronicle', summary: form.shortSummary || undefined },
    { dirty },
  )

  const type = typeOf(types, form.type)
  const active = entries.filter((e) => e.status === 'active')
  // An entry cannot move under itself or its own descendants
  const parents = useMemo(
    () => active.filter((e) => e.id !== entry?.id && !(entry && ancestorsOf(active, e.id).includes(entry.id))),
    [active, entry],
  )
  const related = links.map((l) => (l.fromId === entry?.id ? l.toId : l.fromId))
  const byId = new Map(entries.map((e) => [e.id, e]))

  const set = (patch: Partial<EntryInput>) => setForm((f) => ({ ...f, ...patch }))

  const save = async (extra: { accept?: boolean; reviewed?: boolean } = {}) => {
    setSaving(true)
    setError(undefined)
    try {
      if (!entry) {
        const resp = await Chronicle.create(form)
        await refreshChronicles()
        if (resp.entry) {
          registry.close(tabId)
          registry.edit(new ChronicleObject(resp.entry))
        }
        return
      }
      const resp = await Chronicle.update({ ...form, id: entry.id, ...extra })
      if (resp.entry) {
        setEntry(resp.entry)
        setForm(inputOf(resp.entry))
        registry.rename(tabId, resp.entry.title)
      }
      await refreshChronicles()
    } catch (err) {
      chronicleLog.fromError(err).warn('cannot save the entry')
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!entry) return
    setConfirmDelete(false)
    try {
      await Chronicle.delete(entry.id)
      await refreshChronicles()
      registry.close(tabId)
    } catch (err) {
      chronicleLog.fromError(err).warn('cannot delete the entry')
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const link = async (toId: string, remove = false) => {
    if (!entry) return
    try {
      const resp = await Chronicle.link({ fromId: entry.id, toId, remove })
      setLinks(resp.links ?? [])
      await refreshChronicles()
    } catch (err) {
      chronicleLog.fromError(err).warn('cannot link the entries')
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  if (loading) return <CircularProgress size={20} sx={{ m: 2 }} />

  return (
    <Box sx={{ height: '100%', overflow: 'auto' }}>
      <Stack spacing={2} sx={{ p: 2, maxWidth: 900 }}>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap', rowGap: 1 }}>
          <Typography variant="h6" sx={{ mr: 1 }}>
            {type?.icon} {form.title || 'New Chronicle'}
          </Typography>
          {entry?.status === 'proposed' && <Chip size="small" color="info" label="Proposed by the memory update" />}
          {entry?.review && <Chip size="small" color="warning" label="Flagged for review" />}
          {entry && !mine && (
            <Chip size="small" color="secondary" label={entry.shared === 'global' ? 'Shared with every tenant · read-only' : 'Shared with you · read-only'} />
          )}
          {entry && mine && entry.shared === 'tenant' && <Chip size="small" variant="outlined" label="Shared with your tenant" />}
          {entry && !entry.indexed && entry.status === 'active' && (
            <Chip size="small" variant="outlined" label="Found by keywords only" title="Not embedded yet, e.g. without an API key; it is embedded once a key is set" />
          )}
          <Box sx={{ flex: 1 }} />
          {entry?.status === 'proposed' && (
            <Button variant="contained" color="success" disabled={saving} onClick={() => void save({ accept: true })}>
              Accept
            </Button>
          )}
          {entry?.review && (
            <Button disabled={saving} onClick={() => void save({ reviewed: true })}>
              Mark reviewed
            </Button>
          )}
          {mine && entry?.status === 'active' && <Button onClick={() => setSharing(true)}>Share…</Button>}
          {mine && (
            <Button variant="contained" disabled={saving || (!!entry && !dirty) || !form.title.trim()} onClick={() => void save()}>
              {entry ? 'Save' : 'Create'}
            </Button>
          )}
          {entry && mine && (
            <Button color="error" onClick={() => setConfirmDelete(true)}>
              {entry.status === 'proposed' ? 'Dismiss' : 'Delete'}
            </Button>
          )}
        </Stack>
        {error && <Alert severity="error">{error}</Alert>}
        {sharing && entry && (
          <ShareChronicleDialog
            entry={entry}
            onClose={() => setSharing(false)}
            onChanged={(e) => setEntry((old) => (old ? { ...old, shared: e.shared } : old))}
          />
        )}

        <Box component="fieldset" disabled={!mine} sx={{ border: 0, p: 0, m: 0, minWidth: 0 }}>
          <Stack spacing={2}>
            <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', rowGap: 1 }}>
              <TextField size="small" label="Title" required value={form.title} onChange={(e) => set({ title: e.target.value })} sx={{ flex: 2, minWidth: 220 }} />
              <TextField size="small" select label="Type" value={form.type} onChange={(e) => set({ type: e.target.value })} sx={{ flex: 1, minWidth: 160 }}>
                {types.map((t) => (
                  <MenuItem key={t.id} value={t.id}>
                    {t.icon} {t.title}
                  </MenuItem>
                ))}
              </TextField>
              <Autocomplete
                size="small"
                sx={{ flex: 2, minWidth: 220 }}
                options={parents}
                value={parents.find((e) => e.id === form.parentId) ?? null}
                getOptionLabel={(e) => `${typeOf(types, e.type)?.icon ?? ''} ${e.title}`}
                isOptionEqualToValue={(a, b) => a.id === b.id}
                onChange={(_, e) => set({ parentId: e?.id })}
                renderInput={(params) => <TextField {...params} label="Parent" helperText="Checking this entry in a conversation includes its parents" />}
              />
            </Stack>
            {type && <TypeFields schema={type.fields} value={form.fields ?? {}} onChange={(fields) => set({ fields })} />}
            <TextField
              size="small"
              label="Short summary"
              multiline
              minRows={2}
              value={form.shortSummary}
              onChange={(e) => set({ shortSummary: e.target.value })}
              helperText="One or two sentences. Context matching reads only this to decide whether a message needs the entry."
            />
            <TextField
              size="small"
              label="Full summary"
              multiline
              minRows={6}
              value={form.fullSummary}
              onChange={(e) => set({ fullSummary: e.target.value })}
              helperText="The core facts, in Markdown. This is what a reply that uses the entry reads."
            />
            <Accordion disableGutters defaultExpanded={!!form.fullContext}>
              <AccordionSummary expandIcon={<ExpandMoreIcon />}>
                <Typography variant="body2">Full context</Typography>
              </AccordionSummary>
              <AccordionDetails>
                <TextField
                  fullWidth
                  size="small"
                  multiline
                  minRows={6}
                  value={form.fullContext}
                  onChange={(e) => set({ fullContext: e.target.value })}
                  helperText="The raw, detailed material. A reply reads it only when there is no full summary."
                  slotProps={{ htmlInput: { style: { fontFamily: 'monospace', fontSize: 13 }, 'aria-label': 'Full context' } }}
                />
              </AccordionDetails>
            </Accordion>
          </Stack>
        </Box>

        {entry && mine && (
          <Stack spacing={1}>
            <Typography variant="subtitle2">Related</Typography>
            <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5, alignItems: 'center' }}>
              {related.map((id) => {
                const other = byId.get(id)
                return (
                  <Chip
                    key={id}
                    size="small"
                    label={`${typeOf(types, other?.type ?? '')?.icon ?? ''} ${other?.title ?? id}`}
                    onClick={other ? () => registry.edit(new ChronicleObject(other)) : undefined}
                    onDelete={() => void link(id, true)}
                  />
                )
              })}
              <Autocomplete
                size="small"
                sx={{ minWidth: 240 }}
                options={active.filter((e) => e.id !== entry.id && !related.includes(e.id))}
                value={null}
                getOptionLabel={(e) => `${typeOf(types, e.type)?.icon ?? ''} ${e.title}`}
                onChange={(_, e) => e && void link(e.id)}
                renderInput={(params) => <TextField {...params} placeholder="Link an entry" />}
              />
            </Stack>
            <Typography variant="caption" color="text.secondary">
              When this entry is used in a conversation, context matching also considers its related entries, parent and children.
            </Typography>
          </Stack>
        )}

        {entry && mine && (
          <Stack spacing={1}>
            <Typography variant="subtitle2">Conversation rounds</Typography>
            <RoundList rounds={rounds} onRestore={(r) => r.previous && set({ shortSummary: r.previous.shortSummary, fullSummary: r.previous.fullSummary })} />
          </Stack>
        )}
      </Stack>

      <Dialog open={confirmDelete} onClose={() => setConfirmDelete(false)}>
        <DialogTitle>{entry?.status === 'proposed' ? 'Dismiss' : 'Delete'} “{entry?.title}”?</DialogTitle>
        <DialogContent>
          <Typography variant="body2">
            Its children move up to its parent, and its related links are removed. This cannot be undone.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmDelete(false)}>Cancel</Button>
          <Button color="error" onClick={() => void remove()}>
            {entry?.status === 'proposed' ? 'Dismiss' : 'Delete'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}

export default ChronicleEditor
