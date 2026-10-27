import { Suspense, useState } from 'react'
import { Box, CircularProgress, IconButton, Stack, Tooltip, Typography } from '@mui/material'
import { useContributions } from '../../workbench/extensions/index.ts'
import { sideViews } from '../../workbench/points.ts'
import type { EditorRegistry } from './EditorRegistry.ts'
import AccountMenu from '../../session/AccountMenu.tsx'

const storageKey = 'workbench.sideView'

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

// The left side bar: a vertical tab per workbench.sideView contribution,
// and the open view beside them. Clicking the open view's tab hides it.
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
        <Stack role="tabpanel" aria-label={open.title} sx={{ width: open.width ?? 260, minHeight: 0 }}>
          <Typography variant="overline" sx={{ px: 1.5, pt: 0.5, color: 'text.secondary' }}>
            {open.title}
          </Typography>
          <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
            <Suspense fallback={<CircularProgress size={16} sx={{ m: 2 }} />}>
              <open.component registry={registry} />
            </Suspense>
          </Box>
        </Stack>
      )}
    </Stack>
  )
}

export default SideBar
