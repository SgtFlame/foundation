import { useRef, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  List,
  ListItemButton,
  ListItemText,
  MenuItem,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import UploadFileIcon from '@mui/icons-material/UploadFile'
import { live, useQuery } from '../../live/index.ts'
import type { Graph, GraphSummary } from '../../services/Cognition.ts'
import type { WorkbenchContext } from '../../workbench/points.ts'
import { useCatalog } from './catalog.ts'
import { CognitionObject } from './CognitionObject.ts'
import { graphIdFrom } from './editing.ts'

// Making a graph: bp.create with a kind and a name
export function NewGraphDialog({ kinds, onClose, onCreated, owner }: { kinds: string[]; onClose: () => void; onCreated: (id: string, name: string) => void; owner?: string }) {
  const { catalog } = useCatalog()
  const [kind, setKind] = useState(kinds[0] ?? 'function')
  const [name, setName] = useState('')
  const [error, setError] = useState<string>()
  const create = async () => {
    const id = graphIdFrom(name)
    try {
      await live.command('bp.create', id, { kind, name: name.trim(), ...(owner ? { owner } : {}) })
      onClose()
      onCreated(id, name.trim())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }
  return (
    <Dialog open onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>New graph</DialogTitle>
      <DialogContent>
        <Stack sx={{ gap: 1.5, mt: 1 }}>
          <TextField select size="small" label="Kind" value={kind} onChange={(e) => setKind(e.target.value)} helperText={catalog?.kinds.get(kind)?.description}>
            {kinds.map((k) => (
              <MenuItem key={k} value={k}>
                {catalog?.kinds.get(k)?.title ?? k}
              </MenuItem>
            ))}
          </TextField>
          <TextField autoFocus size="small" label="Name" value={name} onChange={(e) => setName(e.target.value)} />
          {error && <Alert severity="error">{error}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" disabled={!name.trim()} onClick={() => void create()}>
          Create
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// The Explorer side view: the tenant's Cognition graphs and the built-in
// ones, by kind, live; New graph and Import. Prompts and Nexuses join it
// when they exist.
function ExplorerView({ registry }: WorkbenchContext) {
  const { data, error } = useQuery<Record<string, GraphSummary>>('cognition.graphs', {})
  const { catalog } = useCatalog()
  const [creating, setCreating] = useState(false)
  const [importError, setImportError] = useState<string>()
  const file = useRef<HTMLInputElement>(null)
  const graphs = Object.values(data ?? {}).sort((a, b) => a.name.localeCompare(b.name))
  const kinds = [...new Set(graphs.map((g) => g.kind))].sort()
  const open = (id: string, name: string) => registry.edit(new CognitionObject({ id, name }))

  const importFile = async (f: File) => {
    try {
      const graph = JSON.parse(await f.text()) as Graph
      const id = graphIdFrom(graph.name || f.name.replace(/\.json$/, ''))
      await live.command('bp.import', id, { graph })
      setImportError(undefined)
      open(id, graph.name || id)
    } catch (err) {
      setImportError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Box sx={{ height: '100%', overflow: 'auto' }}>
      <Stack direction="row" sx={{ px: 1, pt: 0.5, gap: 0.5 }}>
        <Tooltip title="New graph">
          <IconButton size="small" aria-label="New graph" onClick={() => setCreating(true)}>
            <AddIcon fontSize="small" />
          </IconButton>
        </Tooltip>
        <Tooltip title="Import a graph from JSON">
          <IconButton size="small" aria-label="Import a graph" onClick={() => file.current?.click()}>
            <UploadFileIcon fontSize="small" />
          </IconButton>
        </Tooltip>
        <input
          ref={file}
          type="file"
          accept="application/json,.json"
          hidden
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) void importFile(f)
            e.target.value = ''
          }}
        />
      </Stack>
      {error && <Alert severity="error">{error.message}</Alert>}
      {importError && (
        <Alert severity="error" onClose={() => setImportError(undefined)}>
          {importError}
        </Alert>
      )}
      {data && graphs.length === 0 && (
        <Typography variant="body2" sx={{ px: 1.5, color: 'text.secondary' }}>
          No graphs.
        </Typography>
      )}
      {kinds.map((kind) => (
        <List
          key={kind}
          dense
          disablePadding
          subheader={
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', px: 1.5, pt: 1 }}>
              {catalog?.kinds.get(kind)?.title ?? kind}
            </Typography>
          }
        >
          {graphs
            .filter((g) => g.kind === kind)
            .map((g) => (
              <ListItemButton key={g.id} onClick={() => open(g.id, g.name)} title={g.description}>
                <ListItemText
                  primary={g.name}
                  secondary={[g.published ? `v${g.published}` : 'draft', g.changed ? 'changed' : '', g.readOnly ? 'built in' : g.builtIn ? 'built in, editable' : '']
                    .filter(Boolean)
                    .join(' · ')}
                  slotProps={{ secondary: { variant: 'caption' } }}
                />
              </ListItemButton>
            ))}
        </List>
      ))}
      {creating && catalog && <NewGraphDialog kinds={[...catalog.kinds.keys()]} onClose={() => setCreating(false)} onCreated={open} />}
    </Box>
  )
}

export default ExplorerView
