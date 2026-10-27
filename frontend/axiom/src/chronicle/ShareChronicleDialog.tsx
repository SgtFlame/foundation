// Sharing one of your Chronicle entries (documentation/plans/MultiTenant.md
// › Sharing Chronicles): with your tenant at once, or with every tenant once
// a moderator of your tenant and then a global moderator approve. Others
// may use a shared entry but never change it; the global copy is what you
// asked to share, and changing the entry later does not change it.

import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Stack, Switch, TextField, Typography } from '@mui/material'
import { Chronicle, type Entry, type Share } from '../services/Chronicle.ts'
import { shareStatusText } from './shareStatus.ts'
import { refreshChronicles } from './chronicleStore.ts'

function ShareChronicleDialog({ entry, onClose, onChanged }: { entry: Entry; onClose: () => void; onChanged: (e: Entry) => void }) {
  const [share, setShare] = useState<Share>()
  const [note, setNote] = useState('')
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    Chronicle.shares()
      .then((r) => setShare(r.shares.find((s) => s.entryId === entry.id)))
      .catch((err: unknown) => setError((err as Error).message))
  }, [entry.id])
  useEffect(load, [load])

  const run = async (op: () => Promise<{ entry?: Entry }>) => {
    setBusy(true)
    setError(undefined)
    try {
      const resp = await op()
      if (resp.entry) onChanged(resp.entry)
      load()
      await refreshChronicles()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const open = share && (share.status === 'pending' || share.status === 'tenantApproved' || share.status === 'approved')
  const withTenant = entry.shared === 'tenant'

  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>Share “{entry.title}”</DialogTitle>
      <DialogContent>
        <Stack spacing={2}>
          <FormControlLabel
            control={
              <Switch
                checked={withTenant}
                disabled={busy}
                onChange={(e) =>
                  void run(async () => {
                    if (e.target.checked) return Chronicle.share(entry.id, 'tenant')
                    // Stopping takes back a global share too; keep it if there is one
                    const resp = await Chronicle.share(entry.id, '')
                    return resp
                  })
                }
              />
            }
            label="Share with my tenant: its other members can use it at once, without changing it"
          />
          <Typography variant="subtitle2" component="h3">
            With every tenant
          </Typography>
          {share && (
            <Alert severity={share.status === 'approved' ? 'success' : share.status === 'rejected' ? 'warning' : 'info'}>
              {shareStatusText[share.status]}
              {share.status === 'rejected' && share.note ? `: ${share.note}` : ''}
            </Alert>
          )}
          {open ? (
            <Button
              color="warning"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  const resp = await Chronicle.share(entry.id, '')
                  return withTenant ? Chronicle.share(entry.id, 'tenant') : resp
                })
              }
            >
              {share?.status === 'approved' ? 'Stop sharing with every tenant' : 'Withdraw the request'}
            </Button>
          ) : (
            <Stack spacing={1}>
              <Typography variant="body2" color="text.secondary">
                A moderator of your tenant, then a global moderator, read what you share and approve it. Other tenants then use a copy of
                its title and summaries, not its full context.
              </Typography>
              <TextField size="small" label="Why it is useful to everyone (optional)" value={note} onChange={(e) => setNote(e.target.value)} />
              <Button variant="outlined" disabled={busy} onClick={() => void run(() => Chronicle.share(entry.id, 'global', note))}>
                Ask to share with every tenant
              </Button>
            </Stack>
          )}
          {error && <Alert severity="error">{error}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Done</Button>
      </DialogActions>
    </Dialog>
  )
}

export default ShareChronicleDialog
