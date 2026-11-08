// Undo and Redo on a chat round (forge 06b): the round's commands are one
// request in the operation log (round-<roundId>), so Undo takes back what
// the round changed and Redo brings it back. Each first shows what it would
// do, for the user to confirm.

import { useState } from 'react'
import { Alert, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Stack, Typography } from '@mui/material'
import UndoIcon from '@mui/icons-material/Undo'
import RedoIcon from '@mui/icons-material/Redo'
import { runUndo, useQuery, withRest } from '../../live/index.ts'
import type { RequestStatus, UndoResult } from '../../services/Apps.ts'
import { UndoPlan } from '../undo/UndoButtons.tsx'
import type { ChatRound } from './useChatStream.ts'

// Whether a round may have changed definitions: a write tool ran in it
export function changedSomething(round: ChatRound): boolean {
  return !round.id.startsWith('local-') && !!round.toolCalls?.some((c) => c.effect === 'write' && c.status === 'done')
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export default function RoundUndo({ round }: { round: ChatRound }) {
  const request = `round-${round.id}`
  const { data: status } = useQuery<RequestStatus>('ops.status', { request })
  const [open, setOpen] = useState<'undo' | 'redo'>()
  const [preview, setPreview] = useState<UndoResult>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  if (!status || status.undoable === 0) return null

  const call = (which: 'undo' | 'redo') => (which === 'undo' ? 'ops.undo' : 'ops.redo')
  const show = async (which: 'undo' | 'redo') => {
    setOpen(which)
    setPreview(undefined)
    setError(undefined)
    try {
      setPreview(await withRest(call(which), await runUndo(call(which), { request, dryRun: true })))
    } catch (err) {
      setError(message(err))
    }
  }
  const confirm = async () => {
    if (!open || !preview) return
    setBusy(true)
    try {
      const res = await runUndo(call(open), { request, force: preview.status === 'conflicts' })
      if (res.status === 'conflicts') setPreview(await withRest(call(open), res))
      else setOpen(undefined)
    } catch (err) {
      setError(message(err))
    } finally {
      setBusy(false)
    }
  }
  const verb = open === 'redo' ? 'Redo' : 'Undo'
  const conflicts = preview?.status === 'conflicts'
  return (
    <>
      {status.undone ? (
        <Button size="small" startIcon={<RedoIcon />} onClick={() => void show('redo')} aria-label="Redo this round's changes">
          Redo changes
        </Button>
      ) : (
        <Button size="small" startIcon={<UndoIcon />} onClick={() => void show('undo')} aria-label="Undo this round's changes">
          Undo changes
        </Button>
      )}
      <Dialog open={!!open} onClose={() => setOpen(undefined)} maxWidth="sm" fullWidth>
        <DialogTitle>{verb} what this round changed</DialogTitle>
        <DialogContent>
          {!preview && !error && (
            <Stack sx={{ alignItems: 'center', py: 2 }}>
              <CircularProgress size={24} />
            </Stack>
          )}
          {preview && preview.status === 'nothing' && <Typography variant="body2">There is nothing left to {verb.toLowerCase()}.</Typography>}
          {preview && preview.status !== 'nothing' && <UndoPlan result={preview} />}
          {conflicts && (
            <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
              {verb} the rest to leave those items as the others left them.
            </Typography>
          )}
          {error && (
            <Alert severity="error" sx={{ mt: 1 }}>
              {error}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpen(undefined)}>Cancel</Button>
          <Button variant="contained" disabled={busy || !preview || preview.status === 'nothing' || preview.ops.length === 0} onClick={() => void confirm()}>
            {conflicts ? `${verb} the rest` : verb}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  )
}
