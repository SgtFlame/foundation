import { useState, type ReactNode } from 'react'
import { Collapse, IconButton, List, ListItemButton, ListItemText, Stack, Tooltip } from '@mui/material'
import ExpandLess from '@mui/icons-material/ExpandLess'
import ExpandMore from '@mui/icons-material/ExpandMore'
import { useContributions } from '../../workbench/extensions/index.ts'
import { commands, type CommandContribution, type WorkbenchContext } from '../../workbench/points.ts'
import { useSession } from '../../session/session.ts'
import { useCan } from '../../session/permissions.ts'

function Section({ title, children }: { title: string; children: ReactNode }) {
  const [open, setOpen] = useState(true)

  return (
    <>
      <ListItemButton onClick={() => setOpen((open) => !open)}>
        <ListItemText primary={title} />
        {open ? <ExpandLess /> : <ExpandMore />}
      </ListItemButton>
      <Collapse in={open}>
        <Stack direction="row" sx={{ px: 1, pb: 1, flexWrap: 'wrap' }}>
          {children}
        </Stack>
      </Collapse>
    </>
  )
}

// The Editors side view: every workbench.command with a section, grouped by
// section in the order of their first command. A command's when() sees
// { can, tenantId }: useCan's check of what the user may do (canIn reads
// it), and the active tenant ('1' in single-user mode).
function EditorsView({ registry }: WorkbenchContext) {
  const s = useSession()
  const can = useCan()
  const all = useContributions(commands, { can, tenantId: s.account?.tenantId ?? '1' })
  const sections = new Map<string, CommandContribution[]>()
  for (const c of all) {
    if (c.section) sections.set(c.section, [...(sections.get(c.section) ?? []), c])
  }
  return (
    <List disablePadding>
      {[...sections].map(([section, list]) => (
        <Section key={section} title={section}>
          {list.map((c) => (
            <Tooltip key={c.id} title={c.title}>
              <IconButton aria-label={c.title} onClick={() => c.run({ registry })}>
                {c.icon ? <c.icon /> : c.title.slice(0, 1)}
              </IconButton>
            </Tooltip>
          ))}
        </Section>
      ))}
    </List>
  )
}

export default EditorsView
