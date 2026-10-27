// The moderation queue (documentation/plans/MultiTenant.md › Sharing
// Chronicles): requests to share a Chronicle entry with every tenant. A
// moderator of the owner's tenant approves first, then a global moderator,
// whose approval copies the entry into the global tenant. Each request
// shows what was asked to be shared; nobody moderates their own.

import { useCallback, useEffect, useState } from 'react'
import { Alert, Box, Button, Card, CardActions, CardContent, Chip, Stack, TextField, Typography } from '@mui/material'
import RefreshIcon from '@mui/icons-material/Refresh'
import ToolbarAction from '../../components/folders/ToolbarAction.tsx'
import MarkdownText from '../../components/markdown/MarkdownText.tsx'
import { shareStatusText } from '../../chronicle/shareStatus.ts'
import { typeOf, useChronicles } from '../../chronicle/chronicleStore.ts'
import { Chronicle, type Share } from '../../services/Chronicle.ts'

function Request({ share, onDone, fail }: { share: Share; onDone: () => void; fail: (err: unknown) => void }) {
  const { types } = useChronicles()
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const decide = async (approve: boolean) => {
    setBusy(true)
    try {
      await Chronicle.moderate(share.id, approve, note)
      onDone()
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }
  const stage = share.status === 'pending' ? 'Your tenant’s approval' : 'Global approval'

  return (
    <Card variant="outlined">
      <CardContent>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 1, flexWrap: 'wrap', rowGap: 0.5 }}>
          <Typography variant="subtitle1" component="h3">
            {typeOf(types, share.type)?.icon} {share.title}
          </Typography>
          <Chip size="small" label={stage} color={share.status === 'pending' ? 'default' : 'primary'} variant="outlined" />
          <Box sx={{ flex: 1 }} />
          <Typography variant="caption" color="text.secondary">
            {share.ownerName || `User ${share.ownerId}`} · {new Date(share.created).toLocaleString()}
          </Typography>
        </Stack>
        {share.note && (
          <Alert severity="info" icon={false} sx={{ mb: 1 }}>
            “{share.note}”
          </Alert>
        )}
        <Typography variant="body2" sx={{ mb: 1 }}>
          {share.shortSummary}
        </Typography>
        {share.fullSummary && (
          <Box sx={{ borderLeft: 3, borderColor: 'divider', pl: 1.5 }}>
            <MarkdownText text={share.fullSummary} />
          </Box>
        )}
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
          {shareStatusText[share.status]}
        </Typography>
      </CardContent>
      <CardActions sx={{ px: 2, pb: 2, gap: 1, flexWrap: 'wrap' }}>
        <TextField size="small" label="Reason, if rejecting" value={note} onChange={(e) => setNote(e.target.value)} sx={{ flex: 1, minWidth: 220 }} />
        <Button color="error" disabled={busy} onClick={() => void decide(false)}>
          Reject
        </Button>
        <Button variant="contained" color="success" disabled={busy} onClick={() => void decide(true)}>
          Approve
        </Button>
      </CardActions>
    </Card>
  )
}

function ModerationEditor() {
  const [shares, setShares] = useState<Share[]>([])
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState<string>()
  const fail = useCallback((err: unknown) => setError((err as Error).message), [])
  const load = useCallback(() => {
    Chronicle.shares(true)
      .then((r) => {
        setShares(r.shares)
        setError(undefined)
      }, fail)
      .finally(() => setLoaded(true))
  }, [fail])
  useEffect(load, [load])

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Stack direction="row" sx={{ px: 1, borderBottom: 1, borderColor: 'divider', alignItems: 'center' }}>
        <ToolbarAction title="Refresh" onClick={load}>
          <RefreshIcon />
        </ToolbarAction>
        <Typography variant="body2" color="text.secondary" sx={{ ml: 1 }}>
          Chronicle entries asked to be shared with every tenant
        </Typography>
      </Stack>
      {error && (
        <Alert severity="error" onClose={() => setError(undefined)} sx={{ m: 1 }}>
          {error}
        </Alert>
      )}
      <Stack spacing={1.5} sx={{ p: 1.5, overflow: 'auto', flex: 1, maxWidth: 900 }}>
        {shares.map((s) => (
          <Request key={s.id} share={s} onDone={load} fail={fail} />
        ))}
        {loaded && shares.length === 0 && !error && (
          <Typography variant="body2" color="text.secondary">
            Nothing is waiting for you.
          </Typography>
        )}
      </Stack>
    </Stack>
  )
}

export default ModerationEditor
