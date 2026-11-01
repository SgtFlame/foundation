import { useState } from 'react'
import {
  Alert,
  Badge,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  IconButton,
  List,
  ListItemButton,
  ListItemText,
  Popover,
  Stack,
  TextField,
  Tooltip,
} from '@mui/material'
import UndoIcon from '@mui/icons-material/Undo'
import RedoIcon from '@mui/icons-material/Redo'
import DeleteIcon from '@mui/icons-material/Delete'
import FactCheckIcon from '@mui/icons-material/FactCheck'
import PublishIcon from '@mui/icons-material/Publish'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import DownloadIcon from '@mui/icons-material/Download'
import { live, PresenceAvatars } from '../../live/index.ts'
import { titleOf, useCognition } from './cognitionState.ts'
import { graphIdFrom } from './editing.ts'

// Duplicating a graph: bp.create with from, then its editor opens
function DuplicateDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const state = useCognition()
  const [name, setName] = useState(`Copy of ${state.graph.name}`)
  const [error, setError] = useState<string>()
  const duplicate = async () => {
    const id = graphIdFrom(name)
    try {
      await live.command('bp.create', id, { from: state.graphId, name })
      onClose()
      state.openGraph?.(id, name)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }
  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Duplicate {state.graph.name}</DialogTitle>
      <DialogContent>
        <TextField autoFocus fullWidth size="small" label="Name" value={name} onChange={(e) => setName(e.target.value)} sx={{ mt: 1 }} />
        {error && (
          <Alert severity="error" sx={{ mt: 1 }}>
            {error}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" onClick={() => void duplicate()} disabled={!name.trim()}>
          Duplicate
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// The Logic Editor's toolbar: undo and redo (this user's own changes),
// delete, Validate (the problems), Publish, Duplicate, Export, and who else
// is here
function Toolbar({ onDelete, canDelete }: { onDelete: () => void; canDelete: boolean }) {
  const state = useCognition()
  const { view, editing, readOnly, problems, others } = state
  const [problemsAt, setProblemsAt] = useState<HTMLElement | null>(null)
  const [duplicating, setDuplicating] = useState(false)
  const errors = problems.filter((p) => p.severity === 'error').length
  const changed = view.rev !== view.publishedRev && view.rev > 0
  const publish = () => void editing.send('bp.publish', { rev: view.rev }).catch(() => undefined)
  const exportGraph = () => {
    const blob = new Blob([JSON.stringify(view.graph, null, 2)], { type: 'application/json' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `${state.graphId}.json`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  return (
    <Stack direction="row" sx={{ alignItems: 'center', gap: 0.5, px: 1, py: 0.5, borderBottom: 1, borderColor: 'divider', flexWrap: 'wrap' }}>
      <Tooltip title="Undo your last change (Ctrl+Z)">
        <span>
          <IconButton size="small" aria-label="Undo" onClick={editing.undo} disabled={readOnly || !editing.canUndo}>
            <UndoIcon fontSize="small" />
          </IconButton>
        </span>
      </Tooltip>
      <Tooltip title="Redo (Ctrl+Shift+Z)">
        <span>
          <IconButton size="small" aria-label="Redo" onClick={editing.redo} disabled={readOnly || !editing.canRedo}>
            <RedoIcon fontSize="small" />
          </IconButton>
        </span>
      </Tooltip>
      <Tooltip title="Delete the selection (Delete)">
        <span>
          <IconButton size="small" aria-label="Delete" onClick={onDelete} disabled={readOnly || !canDelete}>
            <DeleteIcon fontSize="small" />
          </IconButton>
        </span>
      </Tooltip>
      <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />
      <Tooltip title={problems.length ? `${problems.length} problems` : 'No problems'}>
        <IconButton size="small" aria-label="Validate" onClick={(e) => setProblemsAt(e.currentTarget)}>
          <Badge badgeContent={problems.length} color={errors ? 'error' : 'warning'}>
            <FactCheckIcon fontSize="small" />
          </Badge>
        </IconButton>
      </Tooltip>
      <Tooltip title={errors ? 'Fix the errors first' : changed || view.published === 0 ? `Publish rev ${view.rev} as version ${view.published + 1}` : 'Nothing new to publish'}>
        <span>
          <Button size="small" startIcon={<PublishIcon />} onClick={publish} disabled={readOnly || errors > 0 || !(changed || (view.published === 0 && view.rev > 0))}>
            Publish
          </Button>
        </span>
      </Tooltip>
      <Chip
        size="small"
        variant="outlined"
        label={view.published ? `v${view.published}${changed ? ' · changed' : ''}` : 'not published'}
        color={changed ? 'warning' : 'default'}
      />
      {view.readOnly && <Chip size="small" label="Built in, read-only: duplicate it to edit" />}
      <Tooltip title="Duplicate">
        <IconButton size="small" aria-label="Duplicate" onClick={() => setDuplicating(true)}>
          <ContentCopyIcon fontSize="small" />
        </IconButton>
      </Tooltip>
      <Tooltip title="Export as JSON">
        <IconButton size="small" aria-label="Export" onClick={exportGraph}>
          <DownloadIcon fontSize="small" />
        </IconButton>
      </Tooltip>
      <Box sx={{ flexGrow: 1 }} />
      <PresenceAvatars users={others} />
      <Popover open={!!problemsAt} anchorEl={problemsAt} onClose={() => setProblemsAt(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}>
        <List dense sx={{ maxWidth: 420, maxHeight: 360, overflow: 'auto' }}>
          {problems.length === 0 && <Alert severity="success">The graph is valid.</Alert>}
          {problems.map((p, i) => (
            <ListItemButton
              key={i}
              onClick={() => {
                if (p.nodeId) {
                  state.select({ nodeId: p.nodeId })
                  state.setSelectedNodes([p.nodeId])
                }
                setProblemsAt(null)
              }}
            >
              <ListItemText
                primary={p.message}
                secondary={p.nodeId ? `${p.severity} · ${titleOf(state, p.nodeId)}${p.pin ? ` · ${p.pin}` : ''}` : p.severity}
                slotProps={{ primary: { color: p.severity === 'error' ? 'error' : 'warning.main', variant: 'body2' } }}
              />
            </ListItemButton>
          ))}
        </List>
      </Popover>
      {duplicating && <DuplicateDialog open onClose={() => setDuplicating(false)} />}
    </Stack>
  )
}

export default Toolbar
