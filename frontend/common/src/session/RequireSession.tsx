import { useEffect, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { Box, CircularProgress } from '@mui/material'
import { session, useSession } from './session.ts'
import { startLogForwarding } from '../logging/forwarder.ts'

// Renders children once there is a session (or no sign-in is required), and
// sends the browser to /login otherwise, coming back here afterwards
function RequireSession({ children }: { children: ReactNode }) {
  const s = useSession()
  const location = useLocation()

  useEffect(() => {
    if (session.get().status === 'loading') void session.load()
  }, [])
  // Records from the forwarding level up go to the server's log storage too
  useEffect(() => {
    if (s.status === 'signedIn' || s.status === 'singleUser') startLogForwarding()
  }, [s.status])

  if (s.status === 'loading') {
    return (
      <Box sx={{ display: 'grid', placeItems: 'center', height: '100vh' }}>
        <CircularProgress aria-label="Loading the session" />
      </Box>
    )
  }
  if (s.status === 'signedOut') {
    const next = location.pathname + location.search
    return <Navigate to={`/login?next=${encodeURIComponent(next)}`} replace />
  }
  return children
}

export default RequireSession
