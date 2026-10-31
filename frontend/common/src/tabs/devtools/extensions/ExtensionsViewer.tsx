import { useCallback, useEffect, useState, useSyncExternalStore } from 'react'
import { Alert, Box, Chip, IconButton, Stack, Tooltip, Typography } from '@mui/material'
import RefreshIcon from '@mui/icons-material/Refresh'
import { log } from '../../../logging/index.ts'
import { Extensions, type PointInfo } from '../../../services/Extensions.ts'
import { defaultOrder, extensions } from '../../../workbench/extensions/index.ts'

interface Row {
  id: string
  description: string
  contributions: { id: string; order: number; source?: string }[]
}

function PointList({ title, points }: { title: string; points: readonly Row[] }) {
  return (
    <Box sx={{ mb: 2 }}>
      <Typography variant="overline" sx={{ px: 1, color: 'text.secondary' }}>
        {title}
      </Typography>
      {points.length === 0 && (
        <Typography sx={{ px: 1, fontSize: 12, color: 'text.secondary' }}>No extension points.</Typography>
      )}
      {points.map((point) => (
        <Box key={point.id} sx={{ px: 1, py: 0.75, borderBottom: 1, borderColor: 'divider' }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'baseline' }}>
            <Box component="span" sx={{ fontFamily: 'monospace', fontWeight: 600, fontSize: 12 }}>
              {point.id}
            </Box>
            <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>{point.description}</Typography>
          </Stack>
          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5, mt: 0.5 }}>
            {point.contributions.length === 0 && (
              <Typography sx={{ fontSize: 12, color: 'text.secondary', fontStyle: 'italic' }}>
                nothing contributes yet
              </Typography>
            )}
            {point.contributions.map((c) => (
              <Tooltip key={c.id} title={`order ${c.order}${c.source ? `, from ${c.source}` : ''}`}>
                <Chip size="small" variant="outlined" label={c.source ? `${c.id} · ${c.source}` : c.id} />
              </Tooltip>
            ))}
          </Stack>
        </Box>
      ))}
    </Box>
  )
}

// Every extension point in the browser and on the server, with what contributes to each
function ExtensionsViewer() {
  const browser = useSyncExternalStore(extensions.subscribe, extensions.snapshot)
  const [server, setServer] = useState<PointInfo[]>()
  const [error, setError] = useState<string>()

  const load = useCallback(() => {
    Extensions.listPoints()
      .then((resp) => {
        setError(undefined)
        setServer(resp.points ?? [])
      })
      .catch((err: unknown) => {
        log.In('extensions').fromError(err).warn('cannot list the server extension points')
        setError(err instanceof Error ? err.message : String(err))
      })
  }, [])
  useEffect(load, [load])

  const browserRows: Row[] = browser.map((point) => ({
    ...point,
    contributions: point.contributions.map((c) => ({ id: c.id, order: c.order ?? defaultOrder, source: c.source })),
  }))

  return (
    <Box sx={{ height: '100%', overflow: 'auto' }}>
      <Stack direction="row" sx={{ justifyContent: 'flex-end', px: 0.5 }}>
        <Tooltip title="Reload the server's extension points">
          <IconButton size="small" onClick={load} aria-label="Reload the server's extension points">
            <RefreshIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Stack>
      <PointList title="Browser" points={browserRows} />
      {error ? (
        <Alert severity="warning" sx={{ mx: 1 }}>
          Server extension points are unavailable: {error}
        </Alert>
      ) : (
        server && <PointList title="Server" points={server} />
      )}
    </Box>
  )
}

export default ExtensionsViewer
