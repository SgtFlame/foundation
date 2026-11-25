import { useState } from 'react'
import {
  Box, Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle, Divider, IconButton,
  ListItemIcon, ListItemText, ListSubheader, Menu, MenuItem, TextField, Tooltip,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import ArrowDownwardIcon from '@mui/icons-material/ArrowDownward'
import ArrowUpwardIcon from '@mui/icons-material/ArrowUpward'
import CheckIcon from '@mui/icons-material/Check'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import DeleteIcon from '@mui/icons-material/Delete'
import EditIcon from '@mui/icons-material/Edit'
import { log } from '../logging/Logger.ts'
import { nameProblem, type Workspace } from './current.ts'
import { useWorkspace, workspaces } from './workspaceStore.ts'

const menuLog = log.In('workspace').Tags('menu')

// The letters shown for a workspace in the narrow side bar
export function workspaceInitials(name: string): string {
  const words = name.trim().split(/\s+/).filter((w) => /[\p{L}\p{N}]/u.test(w))
  const letter = (w?: string) => [...(w ?? '')].find((c) => /[\p{L}\p{N}]/u.test(c)) ?? ''
  return (letter(words[0]) + (words.length > 1 ? letter(words[words.length - 1]) : '')).toUpperCase() || '?'
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// Asks for a workspace's name, checked as the server checks it
function NameDialog({ title, action, initial, list, self, onClose, onSave }: {
  title: string
  action: string
  initial: string
  list: readonly Workspace[]
  self?: string
  onClose: () => void
  onSave: (name: string) => Promise<void>
}) {
  const [name, setName] = useState(initial)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const problem = nameProblem(name, list, self)
  const save = async () => {
    if (problem || busy) return
    setBusy(true)
    try {
      await onSave(name)
      onClose()
    } catch (err) {
      setError(messageOf(err))
      setBusy(false)
    }
  }
  return (
    <Dialog open onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>{title}</DialogTitle>
      <DialogContent>
        <TextField
          autoFocus fullWidth margin="dense" label="Name" value={name}
          onChange={(e) => { setName(e.target.value); setError(undefined) }}
          onKeyDown={(e) => e.key === 'Enter' && void save()}
          error={!!error || (!!problem && name !== initial)}
          helperText={error ?? (name !== initial ? problem : undefined) ?? ' '}
          slotProps={{ htmlInput: { 'aria-label': 'Workspace name' } }}
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button onClick={() => void save()} disabled={!!problem || busy}>{action}</Button>
      </DialogActions>
    </Dialog>
  )
}

type Asking = { kind: 'new' } | { kind: 'rename'; workspace: Workspace } | { kind: 'delete'; workspace: Workspace }

// The Workspaces button in the side bar, above the account button (forge
// 16a question 6): the current workspace's initials, and a menu that
// switches, makes, renames, duplicates, moves and deletes workspaces.
function WorkspaceMenu() {
  const { workspaces: list, current } = useWorkspace()
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const [asking, setAsking] = useState<Asking>()
  const [error, setError] = useState<string>()
  const close = () => setAnchor(null)
  const index = current ? list.findIndex((w) => w.id === current.id) : -1
  const run = (what: string, fn: () => Promise<unknown>) => {
    close()
    fn().catch((err) => {
      menuLog.fromError(err).warn('cannot %s', what)
      setError(messageOf(err))
    })
  }

  const title = current ? `Workspace: ${current.name}` : 'Workspaces'
  return (
    <>
      <Tooltip title={title} placement="right">
        <IconButton aria-label={title} aria-haspopup="menu" onClick={(e) => setAnchor(e.currentTarget)} sx={{ borderRadius: 0 }}>
          <Box
            sx={{
              width: 26, height: 26, borderRadius: 1, border: 1, borderColor: 'primary.main', color: 'primary.main',
              display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 11, fontWeight: 600, letterSpacing: 0.3,
            }}
          >
            {current ? workspaceInitials(current.name) : '…'}
          </Box>
        </IconButton>
      </Tooltip>
      <Menu
        anchorEl={anchor} open={!!anchor} onClose={close}
        anchorOrigin={{ vertical: 'top', horizontal: 'right' }} transformOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        slotProps={{ list: { 'aria-label': 'Workspaces', dense: true }, paper: { sx: { minWidth: 240 } } }}
      >
        <ListSubheader sx={{ lineHeight: 2.5 }}>Workspaces</ListSubheader>
        {list.map((w) => (
          <MenuItem key={w.id} selected={w.id === current?.id} onClick={() => { close(); workspaces.switchTo(w.id) }}>
            <ListItemIcon>{w.id === current?.id && <CheckIcon fontSize="small" />}</ListItemIcon>
            <ListItemText>{w.name}</ListItemText>
          </MenuItem>
        ))}
        <Divider />
        <MenuItem onClick={() => { close(); setAsking({ kind: 'new' }) }}>
          <ListItemIcon><AddIcon fontSize="small" /></ListItemIcon>
          <ListItemText>New workspace…</ListItemText>
        </MenuItem>
        {current && [
          <MenuItem key="rename" onClick={() => { close(); setAsking({ kind: 'rename', workspace: current }) }}>
            <ListItemIcon><EditIcon fontSize="small" /></ListItemIcon>
            <ListItemText>Rename…</ListItemText>
          </MenuItem>,
          <MenuItem key="duplicate" onClick={() => run('duplicate', () => workspaces.duplicate(current.id))}>
            <ListItemIcon><ContentCopyIcon fontSize="small" /></ListItemIcon>
            <ListItemText>Duplicate</ListItemText>
          </MenuItem>,
          index > 0 && (
            <MenuItem key="up" onClick={() => run('move', () => workspaces.move(current.id, index - 1))}>
              <ListItemIcon><ArrowUpwardIcon fontSize="small" /></ListItemIcon>
              <ListItemText>Move up</ListItemText>
            </MenuItem>
          ),
          index >= 0 && index < list.length - 1 && (
            <MenuItem key="down" onClick={() => run('move', () => workspaces.move(current.id, index + 1))}>
              <ListItemIcon><ArrowDownwardIcon fontSize="small" /></ListItemIcon>
              <ListItemText>Move down</ListItemText>
            </MenuItem>
          ),
          !current.isDefault && (
            <MenuItem key="delete" onClick={() => { close(); setAsking({ kind: 'delete', workspace: current }) }}>
              <ListItemIcon><DeleteIcon fontSize="small" /></ListItemIcon>
              <ListItemText>Delete…</ListItemText>
            </MenuItem>
          ),
        ]}
      </Menu>

      {asking?.kind === 'new' && (
        <NameDialog
          title="New workspace" action="Create" initial="" list={list} onClose={() => setAsking(undefined)}
          onSave={async (name) => { await workspaces.createAndSwitch(name) }}
        />
      )}
      {asking?.kind === 'rename' && (
        <NameDialog
          title="Rename workspace" action="Rename" initial={asking.workspace.name} list={list} self={asking.workspace.id}
          onClose={() => setAsking(undefined)} onSave={(name) => workspaces.rename(asking.workspace.id, name)}
        />
      )}
      {asking?.kind === 'delete' && (
        <Dialog open onClose={() => setAsking(undefined)} maxWidth="xs">
          <DialogTitle>Delete {asking.workspace.name}?</DialogTitle>
          <DialogContent>
            <DialogContentText>Its chats stay in your other workspaces.</DialogContentText>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setAsking(undefined)}>Cancel</Button>
            <Button color="error" onClick={() => { const w = asking.workspace; setAsking(undefined); run('delete', () => workspaces.remove(w.id)) }}>
              Delete
            </Button>
          </DialogActions>
        </Dialog>
      )}
      {error && (
        <Dialog open onClose={() => setError(undefined)} maxWidth="xs">
          <DialogTitle>That did not work</DialogTitle>
          <DialogContent><DialogContentText>{error}</DialogContentText></DialogContent>
          <DialogActions><Button onClick={() => setError(undefined)}>OK</Button></DialogActions>
        </Dialog>
      )}
    </>
  )
}

export default WorkspaceMenu
