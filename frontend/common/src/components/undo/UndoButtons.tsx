// Undo and Redo for an editor's toolbar (forge 06b), with the summary of
// what each would reverse as its tooltip, and the dialog naming the items
// others changed since when an undo is refused, with Undo the rest.

import { useEffect, useState } from 'react'
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, List, ListItem, ListItemText, Snackbar, Tooltip, Typography } from '@mui/material'
import UndoIcon from '@mui/icons-material/Undo'
import RedoIcon from '@mui/icons-material/Redo'
import type { UndoState } from '../../live/useUndo.ts'
import type { UndoConflict, UndoResult } from '../../services/Apps.ts'
import { Tenants } from '../../services/Tenants.ts'
import { useSession } from '../../session/session.ts'

let names: Promise<Map<string, string>> | undefined

// The tenant's members' names by id, read once; empty when the caller may
// not list them
export function useUserNames(): Map<string, string> {
  const tenant = useSession().account?.tenantId
  const [map, setMap] = useState(new Map<string, string>())
  useEffect(() => {
    if (!tenant) return
    names ??= Tenants.members(tenant)
      .then((r) => new Map(r.users.map((u) => [u.id, u.displayName || u.username])))
      .catch(() => new Map<string, string>())
    void names.then(setMap)
  }, [tenant])
  return map
}

export function whoOf(c: { principal: string; actor?: string }, users: Map<string, string>): string {
  const name = users.get(c.principal) ?? `user ${c.principal}`
  return c.actor ? `${name} (${c.actor})` : name
}

// An item in words: "role viewer of tasks", "node root/title of taskRow"
export function itemOf(c: UndoConflict): string {
  const [, id] = c.item.target.split(':')
  return c.item.kind ? `${c.item.kind} ${c.item.key} of ${id}` : id
}

// What an undo or redo does or would do: the ops it sends, what it skips
// and the conflicts with what others changed since
export function UndoPlan({ result }: { result: UndoResult }) {
  const users = useUserNames()
  return (
    <Box>
      <Typography variant="body2" gutterBottom>
        {result.summary}
      </Typography>
      {result.changes.length > 0 ? (
        <>
          <Typography variant="subtitle2" sx={{ mt: 1 }}>
            It takes back
          </Typography>
          <List dense disablePadding aria-label="What it takes back">
            {result.changes.map((c, i) => (
              <ListItem key={i} disableGutters>
                <ListItemText primary={c} />
              </ListItem>
            ))}
          </List>
        </>
      ) : (
        <Typography variant="body2" color="text.secondary">
          Nothing it can take back.
        </Typography>
      )}
      {result.conflicts.length > 0 && (
        <>
          <Typography variant="subtitle2" sx={{ mt: 1 }}>
            Changed since by someone else
          </Typography>
          <List dense disablePadding>
            {result.conflicts.map((c, i) => (
              <ListItem key={i} disableGutters>
                <ListItemText primary={itemOf(c)} secondary={`${c.command} by ${whoOf(c, users)}, ${new Date(c.time).toLocaleString()}`} />
              </ListItem>
            ))}
          </List>
        </>
      )}
      {result.skipped.filter((s) => !s.why.startsWith('changed since')).length > 0 && (
        <>
          <Typography variant="subtitle2" sx={{ mt: 1 }}>
            Not undone
          </Typography>
          <List dense disablePadding>
            {result.skipped
              .filter((s) => !s.why.startsWith('changed since'))
              .map((s, i) => (
                <ListItem key={i} disableGutters>
                  <ListItemText primary={`${s.command} (${s.target})`} secondary={s.why} />
                </ListItem>
              ))}
          </List>
        </>
      )}
    </Box>
  )
}

// The dialog of a refused undo: what others changed, with Undo the rest
// (an editor's Undo, or an Activity tab's Undo request)
export function UndoConflictDialog({ state }: { state: Pick<UndoState, 'conflict' | 'dismiss' | 'force' | 'busy'> }) {
  const c = state.conflict
  return (
    <Dialog open={!!c} onClose={state.dismiss} maxWidth="sm" fullWidth>
      <DialogTitle>{c?.redo ? 'Redo' : 'Undo'}: others changed this since</DialogTitle>
      <DialogContent>
        {c && <UndoPlan result={c.result} />}
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          {c?.redo ? 'Redo the rest' : 'Undo the rest'} to leave those items as the others left them.
        </Typography>
      </DialogContent>
      <DialogActions>
        <Button onClick={state.dismiss}>Cancel</Button>
        <Button variant="contained" onClick={() => void state.force()} disabled={state.busy || !c?.result.ops.length}>
          {c?.redo ? 'Redo the rest' : 'Undo the rest'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// Undo and Redo buttons, the conflict dialog and a failed undo's message
export function UndoButtons({ state, disabled }: { state: UndoState; disabled?: boolean }) {
  return (
    <>
      <Tooltip title={`${state.undoTitle} (Ctrl+Z)`}>
        <span>
          <IconButton size="small" aria-label="Undo" onClick={() => void state.undo()} disabled={disabled || !state.canUndo}>
            <UndoIcon fontSize="small" />
          </IconButton>
        </span>
      </Tooltip>
      <Tooltip title={`${state.redoTitle} (Ctrl+Shift+Z)`}>
        <span>
          <IconButton size="small" aria-label="Redo" onClick={() => void state.redo()} disabled={disabled || !state.canRedo}>
            <RedoIcon fontSize="small" />
          </IconButton>
        </span>
      </Tooltip>
      <UndoConflictDialog state={state} />
      <Snackbar open={!!state.error} autoHideDuration={8000} onClose={state.clearError} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}>
        <Alert severity="error" onClose={state.clearError} variant="filled">
          {state.error}
        </Alert>
      </Snackbar>
    </>
  )
}
