import { useEffect, useState } from 'react'
import { Alert, Box, List, ListItemButton, ListItemText, Typography } from '@mui/material'
import { log } from '../../logging/index.ts'
import { Cognition, type GraphInfo } from '../../services/Cognition.ts'
import type { WorkbenchContext } from '../../workbench/points.ts'
import { CognitionObject } from './CognitionObject.ts'

const cognitionLog = log.In('cognition')

const kindTitles: Record<string, string> = {
  mode: 'Chat modes',
  agent: 'Agents',
  function: 'Functions',
  summarizer: 'Summarizers',
}

// The Explorer side view: the Cognition graphs by kind. Prompts and
// Nexuses join it when they exist.
function ExplorerView({ registry }: WorkbenchContext) {
  const [graphs, setGraphs] = useState<GraphInfo[]>()
  const [error, setError] = useState<string>()
  useEffect(() => {
    Cognition.listGraphs()
      .then((r) => setGraphs(r.graphs))
      .catch((err: unknown) => {
        cognitionLog.fromError(err).warn('cannot list the Cognition graphs')
        setError(err instanceof Error ? err.message : String(err))
      })
  }, [])

  const kinds = [...new Set((graphs ?? []).map((g) => g.kind))]
  return (
    <Box sx={{ height: '100%', overflow: 'auto' }}>
      {error && <Alert severity="error">{error}</Alert>}
      {graphs?.length === 0 && (
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
              {kindTitles[kind] ?? kind}
            </Typography>
          }
        >
          {graphs
            ?.filter((g) => g.kind === kind)
            .map((g) => (
              <ListItemButton key={g.id} onClick={() => registry.edit(new CognitionObject(g))} title={g.description}>
                <ListItemText
                  primary={g.name}
                  secondary={`v${g.version}${g.builtIn ? ' · built in' : ''}`}
                  slotProps={{ secondary: { variant: 'caption' } }}
                />
              </ListItemButton>
            ))}
        </List>
      ))}
    </Box>
  )
}

export default ExplorerView
