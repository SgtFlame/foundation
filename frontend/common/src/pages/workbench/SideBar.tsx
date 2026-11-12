import { Suspense, useState } from 'react'
import { Box, CircularProgress, IconButton, Stack, Tooltip, Typography } from '@mui/material'
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft'
import ChevronRightIcon from '@mui/icons-material/ChevronRight'
import { useContributions } from '../../workbench/extensions/index.ts'
import { sideViews } from '../../workbench/points.ts'
import type { EditorRegistry } from './EditorRegistry.ts'
import AccountMenu from '../../session/AccountMenu.tsx'

const storageKey = 'workbench.sideView'
const collapsedKey = 'workbench.sideView.collapsed'

function remembered(): string | null | undefined {
  try {
    const v = localStorage.getItem(storageKey)
    return v === '' ? null : (v ?? undefined)
  } catch {
    return undefined
  }
}

function remember(id: string | null) {
  try {
    localStorage.setItem(storageKey, id ?? '')
  } catch {
    // the choice is only a convenience
  }
}

// The ids of the collapsible views the user collapsed
function rememberedCollapsed(): string[] {
  try {
    return JSON.parse(localStorage.getItem(collapsedKey) ?? '[]') as string[]
  } catch {
    return []
  }
}

function rememberCollapsed(ids: string[]) {
  try {
    localStorage.setItem(collapsedKey, JSON.stringify(ids))
  } catch {
    // the choice is only a convenience
  }
}

// The left side bar: a vertical tab per workbench.sideView contribution,
// and the open view beside them. Clicking the open view's tab hides it; a
// collapsible view's chevron narrows it to its icons.
function SideBar({ registry }: { registry: EditorRegistry }) {
  const views = useContributions(sideViews)
  // undefined: not chosen yet (the first view); null: hidden
  const [chosen, setChosen] = useState<string | null | undefined>(remembered)
  const open = chosen === null ? undefined : (views.find((v) => v.id === chosen) ?? views[0])
  const choose = (id: string) => {
    const next = open?.id === id ? null : id
    setChosen(next)
    remember(next)
  }
  const [collapsedIds, setCollapsedIds] = useState(rememberedCollapsed)
  const collapsed = !!open?.collapsible && collapsedIds.includes(open.id)
  const toggleCollapsed = (id: string) => {
    const next = collapsedIds.includes(id) ? collapsedIds.filter((c) => c !== id) : [...collapsedIds, id]
    setCollapsedIds(next)
    rememberCollapsed(next)
  }

  return (
    <Stack direction="row" sx={{ flexShrink: 0, borderRight: 1, borderColor: 'divider' }}>
      <Stack sx={{ py: 0.5, borderRight: open ? 1 : 0, borderColor: 'divider' }}>
      <Stack role="tablist" aria-orientation="vertical" aria-label="Side views" sx={{ flex: 1 }}>
        {views.map((v) => (
          <Tooltip key={v.id} title={v.title} placement="right">
            <IconButton
              role="tab"
              aria-selected={open?.id === v.id}
              aria-label={v.title}
              onClick={() => choose(v.id)}
              sx={{ borderRadius: 0, borderLeft: 2, borderColor: open?.id === v.id ? 'primary.main' : 'transparent', color: open?.id === v.id ? 'text.primary' : 'text.secondary' }}
            >
              <v.icon />
            </IconButton>
          </Tooltip>
        ))}
      </Stack>
      <AccountMenu registry={registry} />
      </Stack>
      {open && (
        <Stack role="tabpanel" aria-label={open.title} sx={{ width: collapsed ? undefined : (open.width ?? 260), minHeight: 0 }}>
          <Stack direction="row" sx={{ alignItems: 'center', justifyContent: collapsed ? 'center' : undefined, pl: collapsed ? 0 : 1.5, pt: 0.5 }}>
            {!collapsed && (
              <Typography variant="overline" sx={{ flex: 1, color: 'text.secondary' }}>
                {open.title}
              </Typography>
            )}
            {open.collapsible && (
              <Tooltip title={collapsed ? 'Expand' : 'Collapse'} placement="right">
                <IconButton size="small" aria-label={collapsed ? `Expand ${open.title}` : `Collapse ${open.title}`} onClick={() => toggleCollapsed(open.id)}>
                  {collapsed ? <ChevronRightIcon fontSize="small" /> : <ChevronLeftIcon fontSize="small" />}
                </IconButton>
              </Tooltip>
            )}
          </Stack>
          <Box sx={{ flex: 1, minHeight: 0, overflowY: 'auto', overflowX: 'hidden' }}>
            <Suspense fallback={<CircularProgress size={16} sx={{ m: 2 }} />}>
              <open.component registry={registry} collapsed={collapsed} />
            </Suspense>
          </Box>
        </Stack>
      )}
    </Stack>
  )
}

export default SideBar
