// Shows the sealed page while the server waits to be unsealed or is still
// starting (documentation/plans/Secrets.md › Sealed start), and the app once
// it is ready. A server that cannot be reached at all is left to the pages,
// which already say so.

import { useEffect, useState, type ReactNode } from 'react'
import { Box, CircularProgress, Paper, Stack, Typography } from '@mui/material'
import LockIcon from '@mui/icons-material/Lock'
import { log } from '../logging/index.ts'
import { Bootstrap, type BootstrapState } from '../services/Bootstrap.ts'

const bootLog = log.In('secrets').Tags('bootstrap')

function SealedGate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<BootstrapState | 'unknown'>()

  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const poll = () => {
      Bootstrap.status()
        .then((r) => r.state, (err: unknown) => {
          bootLog.fromError(err).debug('cannot read the bootstrap state')
          return 'unknown' as const
        })
        .then((s) => {
          if (stopped) return
          setState(s)
          if (s === 'sealed' || s === 'starting') timer = setTimeout(poll, 3000)
        })
    }
    poll()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [])

  if (state === undefined) return null
  if (state !== 'sealed' && state !== 'starting') return <>{children}</>
  return (
    <Box sx={{ minHeight: '100vh', display: 'grid', placeItems: 'center', bgcolor: 'background.default', p: 2 }}>
      <Paper variant="outlined" sx={{ p: 3, maxWidth: 480 }}>
        <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center', mb: 1.5 }}>
          {state === 'sealed' ? <LockIcon color="warning" /> : <CircularProgress size={22} />}
          <Typography variant="h6" component="h1">
            {state === 'sealed' ? 'The server is sealed' : 'The server is starting'}
          </Typography>
        </Stack>
        <Typography variant="body2" color="text.secondary">
          {state === 'sealed' ? (
            <>
              It keeps its secrets in Vault and is waiting for an operator to unseal it with <code>backend/scripts/secrets/unseal.sh</code>. This page
              continues by itself once it is ready.
            </>
          ) : (
            'It has been unsealed and is opening its databases. This page continues by itself in a moment.'
          )}
        </Typography>
      </Paper>
    </Box>
  )
}

export default SealedGate
