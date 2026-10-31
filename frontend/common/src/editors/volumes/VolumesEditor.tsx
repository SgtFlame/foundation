import { useState } from 'react'
import {
  Alert,
  Box,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Stack,
  Typography,
} from '@mui/material'
import CreateNewFolderIcon from '@mui/icons-material/CreateNewFolder'
import DeleteIcon from '@mui/icons-material/Delete'
import FolderIcon from '@mui/icons-material/Folder'
import LibraryAddIcon from '@mui/icons-material/LibraryAdd'
import LibraryBooksIcon from '@mui/icons-material/LibraryBooks'
import RefreshIcon from '@mui/icons-material/Refresh'
import ShareIcon from '@mui/icons-material/Share'
import FolderBreadcrumbs from '../../components/folders/FolderBreadcrumbs.tsx'
import NameDialog from '../../components/folders/NameDialog.tsx'
import ToolbarAction from '../../components/folders/ToolbarAction.tsx'
import ShareDialog from '../../components/share/ShareDialog.tsx'
import { KnowledgeBase } from '../../services/KnowledgeBase.ts'
import { tenantOf, useCan } from '../../session/permissions.ts'
import { useSession } from '../../session/session.ts'
import { useFolderListing } from '../../components/folders/useFolderListing.ts'
import { log } from '../../logging/index.ts'
import { useWorkbenchSubject } from '../../workbench/context/WorkbenchContext.ts'
import type { EditorProps } from '../../workbench/points.ts'
import { Volume } from '../../services/Volume.ts'
import { openVolume } from '../volume/openVolume.ts'
import type { VolumesObject } from './VolumesObject.ts'

// Navigates the knowledge base root: open folders, make and remove folders,
// convert a folder into a volume, and share a volume (the Share dialog, for
// whoever may grant there). Double-clicking a volume opens it in a
// VolumeEditor.
function VolumesEditor({ registry, tabId }: EditorProps<VolumesObject>) {
  const [folders, setFolders] = useState<string[]>([])
  const [selected, setSelected] = useState<string>()
  useWorkbenchSubject(
    tabId,
    { type: 'folder', id: `/${folders.join('/')}`, label: folders.at(-1) ?? 'Knowledge base', summary: 'A knowledge base folder' },
    { selection: selected ? { items: [selected] } : undefined },
  )
  const [newFolderOpen, setNewFolderOpen] = useState(false)
  const [sharing, setSharing] = useState<{ resource: string; name: string }>()
  const listing = useFolderListing(folders)
  const session = useSession()
  const can = useCan()

  const navigate = (next: string[]) => {
    setFolders(next)
    setSelected(undefined)
    listing.clearError()
  }

  const selectedEntry = listing.folders.find((entry) => entry.name === selected)
  const selectedPath = selected ? { folders: [...folders, selected] } : undefined

  const makeFolder = (name: string) => {
    setNewFolderOpen(false)
    void listing.run(() => Volume.makeFolder({ path: { folders: [...folders, name] } }))
  }

  const removeFolder = () => {
    if (!selectedPath) return
    void listing.run(() => Volume.removeFolder({ path: selectedPath }))
  }

  const createVolume = () => {
    if (!selectedPath) return
    void listing.run(async () => {
      const response = await Volume.createVolume({ path: selectedPath })
      log
        .In('kb')
        .Tags('volume')
        .With('volumeId', response.volumeId)
        .info('created volume %s', selectedPath.folders.join('/'))
    })
  }

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Stack
        direction="row"
        sx={{ alignItems: 'center', gap: 1, px: 1, py: 0.5, borderBottom: 1, borderColor: 'divider' }}
      >
        <FolderBreadcrumbs
          rootLabel="Root"
          folders={folders}
          onNavigate={(depth) => navigate(folders.slice(0, depth))}
        />
        <ToolbarAction title="Refresh" onClick={listing.refresh}>
          <RefreshIcon fontSize="small" />
        </ToolbarAction>
        <ToolbarAction title="New folder" onClick={() => setNewFolderOpen(true)}>
          <CreateNewFolderIcon fontSize="small" />
        </ToolbarAction>
        <ToolbarAction
          title="Remove empty folder"
          disabled={!selectedEntry || selectedEntry.isVolume}
          onClick={removeFolder}
        >
          <DeleteIcon fontSize="small" />
        </ToolbarAction>
        <ToolbarAction
          title="Convert to volume"
          disabled={!selectedEntry || selectedEntry.isVolume}
          onClick={createVolume}
        >
          <LibraryAddIcon fontSize="small" />
        </ToolbarAction>
        <ToolbarAction
          title="Share volume"
          disabled={!selectedEntry?.isVolume || !can('grant.read', `${tenantOf(session)}/kb`)}
          onClick={() =>
            void listing.run(async () => {
              if (!selectedPath || !selected) return
              const { volumeId } = await KnowledgeBase.openVolume({ path: selectedPath })
              if (volumeId) setSharing({ resource: `${tenantOf(session)}/volume:${volumeId}`, name: selected })
            })
          }
        >
          <ShareIcon fontSize="small" />
        </ToolbarAction>
      </Stack>

      {listing.error && (
        <Alert severity="error" onClose={listing.clearError} sx={{ m: 1 }}>
          {listing.error}
        </Alert>
      )}

      <Box sx={{ flexGrow: 1, overflow: 'auto' }}>
        {listing.folders.length === 0 ? (
          <Typography color="text.secondary" sx={{ p: 2 }}>
            No folders
          </Typography>
        ) : (
          <List dense>
            {listing.folders.map((entry) => (
              <ListItemButton
                key={entry.name}
                selected={entry.name === selected}
                onClick={() => setSelected(entry.name)}
                onDoubleClick={() =>
                  entry.isVolume
                    ? void listing.run(() => openVolume(registry, [...folders, entry.name]))
                    : navigate([...folders, entry.name])
                }
              >
                <ListItemIcon>
                  {entry.isVolume ? <LibraryBooksIcon color="primary" /> : <FolderIcon />}
                </ListItemIcon>
                <ListItemText
                  primary={entry.name}
                  secondary={entry.isVolume ? 'Volume' : undefined}
                />
              </ListItemButton>
            ))}
          </List>
        )}
      </Box>

      {sharing && <ShareDialog resource={sharing.resource} title={sharing.name} onClose={() => setSharing(undefined)} />}

      <NameDialog
        open={newFolderOpen}
        title="New folder"
        onClose={() => setNewFolderOpen(false)}
        onCreate={makeFolder}
      />
    </Stack>
  )
}

export default VolumesEditor
