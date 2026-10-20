import { useState, type ReactNode } from 'react'
import {
  Collapse,
  IconButton,
  List,
  ListItemButton,
  ListItemText,
  Stack,
  Tooltip,
} from '@mui/material'
import BarChartIcon from '@mui/icons-material/BarChart'
import DescriptionIcon from '@mui/icons-material/Description'
import ExpandLess from '@mui/icons-material/ExpandLess'
import ExpandMore from '@mui/icons-material/ExpandMore'
import LibraryBooksIcon from '@mui/icons-material/LibraryBooks'
import { BarObject } from '../../editors/bar/BarObject.ts'
import { FooObject } from '../../editors/foo/FooObject.ts'
import { VolumesObject } from '../../editors/volumes/VolumesObject.ts'
import type { EditorRegistry } from './EditorRegistry.ts'

function Section({ title, children }: { title: string; children: ReactNode }) {
  const [open, setOpen] = useState(true)

  return (
    <>
      <ListItemButton onClick={() => setOpen((open) => !open)}>
        <ListItemText primary={title} />
        {open ? <ExpandLess /> : <ExpandMore />}
      </ListItemButton>
      <Collapse in={open}>
        <Stack direction="row" sx={{ px: 1, pb: 1 }}>
          {children}
        </Stack>
      </Collapse>
    </>
  )
}

function LeftMenuSidebar({ registry }: { registry: EditorRegistry }) {
  return (
    <List
      disablePadding
      sx={{ width: 220, flexShrink: 0, borderRight: 1, borderColor: 'divider' }}
    >
      <Section title="One">
        <Tooltip title="Foo">
          <IconButton onClick={() => registry.edit(new FooObject('foo-1'))}>
            <DescriptionIcon />
          </IconButton>
        </Tooltip>
      </Section>
      <Section title="Two">
        <Tooltip title="Bar">
          <IconButton onClick={() => registry.edit(new BarObject('bar-1'))}>
            <BarChartIcon />
          </IconButton>
        </Tooltip>
      </Section>
      <Section title="Knowledge">
        <Tooltip title="Volumes">
          <IconButton
            onClick={() => registry.edit(new VolumesObject('volumes'))}
          >
            <LibraryBooksIcon />
          </IconButton>
        </Tooltip>
      </Section>
    </List>
  )
}

export default LeftMenuSidebar
