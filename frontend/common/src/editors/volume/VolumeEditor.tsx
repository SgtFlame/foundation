import { useState } from 'react'
import {
  Alert,
  Box,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from '@mui/material'
import CreateNewFolderIcon from '@mui/icons-material/CreateNewFolder'
import DescriptionIcon from '@mui/icons-material/Description'
import FolderIcon from '@mui/icons-material/Folder'
import LibraryBooksIcon from '@mui/icons-material/LibraryBooks'
import NoteAddIcon from '@mui/icons-material/NoteAdd'
import RefreshIcon from '@mui/icons-material/Refresh'
import FolderBreadcrumbs from '../../components/folders/FolderBreadcrumbs.tsx'
import NameDialog from '../../components/folders/NameDialog.tsx'
import ToolbarAction from '../../components/folders/ToolbarAction.tsx'
import { useFolderListing } from '../../components/folders/useFolderListing.ts'
import { useWorkbenchSubject } from '../../workbench/context/WorkbenchContext.ts'
import type { EditorProps } from '../../workbench/points.ts'
import { Volume, fileTypes } from '../../services/Volume.ts'
import { openMarkdown } from '../markdown/openMarkdown.ts'
import { openVolume } from './openVolume.ts'
import type { VolumeObject } from './VolumeObject.ts'

function fileLabel(name: string, type: string): string {
  const fileType = fileTypes.find((t) => t.type === type)
  return fileType ? `${name}${fileType.extension}` : name
}

// Browses one volume: open subfolders, and make folders and files
function VolumeEditor({ object, registry, tabId }: EditorProps<VolumeObject>) {
  const [subfolders, setSubfolders] = useState<string[]>([])
  const [selected, setSelected] = useState<string>()
  useWorkbenchSubject(
    tabId,
    { type: 'volume', id: object.tsid, label: object.name, summary: `/${[...object.path, ...subfolders].join('/')}` },
    { selection: selected ? { items: [selected.split(':')[1]] } : undefined },
  )
  const [dialog, setDialog] = useState<'folder' | 'file'>()
  const [fileType, setFileType] = useState<string>(fileTypes[0].type)
  const path = [...object.path, ...subfolders]
  const listing = useFolderListing(path)

  const navigate = (next: string[]) => {
    setSubfolders(next)
    setSelected(undefined)
    listing.clearError()
  }

  const makeFolder = (name: string) => {
    setDialog(undefined)
    void listing.run(() => Volume.makeFolder({ path: { folders: [...path, name] } }))
  }

  const createFile = (name: string) => {
    setDialog(undefined)
    void listing.run(() =>
      Volume.createFile({ path: { folders: [...path, name] }, type: fileType }),
    )
  }

  const empty = listing.folders.length === 0 && listing.files.length === 0

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Stack
        direction="row"
        sx={{ alignItems: 'center', gap: 1, px: 1, py: 0.5, borderBottom: 1, borderColor: 'divider' }}
      >
        <FolderBreadcrumbs
          rootLabel={object.name}
          folders={subfolders}
          onNavigate={(depth) => navigate(subfolders.slice(0, depth))}
        />
        <ToolbarAction title="Refresh" onClick={listing.refresh}>
          <RefreshIcon fontSize="small" />
        </ToolbarAction>
        <ToolbarAction title="New folder" onClick={() => setDialog('folder')}>
          <CreateNewFolderIcon fontSize="small" />
        </ToolbarAction>
        <ToolbarAction title="New file" onClick={() => setDialog('file')}>
          <NoteAddIcon fontSize="small" />
        </ToolbarAction>
      </Stack>

      {listing.error && (
        <Alert severity="error" onClose={listing.clearError} sx={{ m: 1 }}>
          {listing.error}
        </Alert>
      )}

      <Box sx={{ flexGrow: 1, overflow: 'auto' }}>
        {empty ? (
          <Typography color="text.secondary" sx={{ p: 2 }}>
            Empty folder
          </Typography>
        ) : (
          <List dense>
            {listing.folders.map((entry) => (
              <ListItemButton
                key={`folder:${entry.name}`}
                selected={selected === `folder:${entry.name}`}
                onClick={() => setSelected(`folder:${entry.name}`)}
                onDoubleClick={() =>
                  entry.isVolume
                    ? void listing.run(() => openVolume(registry, [...path, entry.name]))
                    : navigate([...subfolders, entry.name])
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
            {listing.files.map((entry) => (
              <ListItemButton
                key={`file:${entry.name}:${entry.type}`}
                selected={selected === `file:${entry.name}:${entry.type}`}
                onClick={() => setSelected(`file:${entry.name}:${entry.type}`)}
                onDoubleClick={() =>
                  entry.type === 'markdown' &&
                  void listing.run(() => openMarkdown(registry, [...path, entry.name], entry.type))
                }
              >
                <ListItemIcon>
                  <DescriptionIcon />
                </ListItemIcon>
                <ListItemText primary={fileLabel(entry.name, entry.type)} />
              </ListItemButton>
            ))}
          </List>
        )}
      </Box>

      <NameDialog
        open={dialog === 'folder'}
        title="New folder"
        onClose={() => setDialog(undefined)}
        onCreate={makeFolder}
      />
      <NameDialog
        open={dialog === 'file'}
        title="New file"
        onClose={() => setDialog(undefined)}
        onCreate={createFile}
      >
        <TextField
          select
          fullWidth
          margin="dense"
          label="Type"
          value={fileType}
          onChange={(event) => setFileType(event.target.value)}
        >
          {fileTypes.map((t) => (
            <MenuItem key={t.type} value={t.type}>
              {t.label} ({t.extension})
            </MenuItem>
          ))}
        </TextField>
      </NameDialog>
    </Stack>
  )
}

export default VolumeEditor
