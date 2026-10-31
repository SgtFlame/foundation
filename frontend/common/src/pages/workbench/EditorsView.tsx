import { useState, type ReactNode } from 'react'
import { Collapse, Divider, List, ListItemButton, ListItemIcon, ListItemText, Tooltip } from '@mui/material'
import ExpandLess from '@mui/icons-material/ExpandLess'
import ExpandMore from '@mui/icons-material/ExpandMore'
import { useContributions } from '../../workbench/extensions/index.ts'
import { commands, type CommandContribution, type SideViewProps } from '../../workbench/points.ts'
import { useSession } from '../../session/session.ts'
import { useCan } from '../../session/permissions.ts'

function Section({ title, collapsed, children }: { title: string; collapsed?: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(true)

  // Collapsed, every section is open and only a divider marks where it starts
  if (collapsed) {
    return (
      <>
        <Divider sx={{ my: 0.5 }} />
        {children}
      </>
    )
  }
  return (
    <>
      <ListItemButton dense onClick={() => setOpen((open) => !open)}>
        <ListItemText primary={title} slotProps={{ primary: { variant: 'body2', color: 'text.secondary' } }} />
        {open ? <ExpandLess fontSize="small" /> : <ExpandMore fontSize="small" />}
      </ListItemButton>
      <Collapse in={open}>
        <List disablePadding>{children}</List>
      </Collapse>
    </>
  )
}

// One command, its icon and title on a line; collapsed, only the icon, with
// the title as its tooltip
function Item({ command: c, collapsed, onClick }: { command: CommandContribution; collapsed?: boolean; onClick: () => void }) {
  const icon = c.icon ? <c.icon fontSize="small" /> : c.title.slice(0, 1)
  if (collapsed) {
    return (
      <Tooltip title={c.title} placement="right">
        <ListItemButton aria-label={c.title} onClick={onClick} sx={{ justifyContent: 'center', px: 1.5 }}>
          <ListItemIcon sx={{ minWidth: 0 }}>{icon}</ListItemIcon>
        </ListItemButton>
      </Tooltip>
    )
  }
  return (
    <ListItemButton dense onClick={onClick} sx={{ pl: 2 }}>
      <ListItemIcon sx={{ minWidth: 32 }}>{icon}</ListItemIcon>
      <ListItemText primary={c.title} />
    </ListItemButton>
  )
}

// The Editors side view: every workbench.command with a section, grouped by
// section in the order of their first command. A command's when() sees
// { can, tenantId }: useCan's check of what the user may do (canIn reads
// it), and the active tenant ('1' in single-user mode).
function EditorsView({ registry, collapsed }: SideViewProps) {
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
        <Section key={section} title={section} collapsed={collapsed}>
          {list.map((c) => (
            <Item key={c.id} command={c} collapsed={collapsed} onClick={() => c.run({ registry })} />
          ))}
        </Section>
      ))}
    </List>
  )
}

export default EditorsView
