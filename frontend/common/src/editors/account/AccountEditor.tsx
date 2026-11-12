import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { Alert, Box, Button, Chip, Divider, List, ListItem, ListItemText, Stack, TextField, Typography } from '@mui/material'
import { Identity } from '../../services/Identity.ts'
import { session, useSession } from '../../session/session.ts'

type Notice = { severity: 'success' | 'error'; text: string } | undefined

function Profile() {
  const { account } = useSession()
  const [displayName, setDisplayName] = useState(account?.user.displayName ?? '')
  const [email, setEmail] = useState(account?.user.email ?? '')
  const [notice, setNotice] = useState<Notice>()
  const changed = displayName !== account?.user.displayName || email !== (account?.user.email ?? '')

  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      session.updated(await Identity.updateAccount({ displayName, email }))
      setNotice({ severity: 'success', text: 'Saved' })
    } catch (err) {
      setNotice({ severity: 'error', text: (err as Error).message })
    }
  }

  return (
    <Stack component="form" spacing={2} onSubmit={save} sx={{ maxWidth: 420 }}>
      <Typography variant="h6" component="h2">Profile</Typography>
      <TextField label="Username" value={account?.user.username ?? ''} disabled helperText="Usernames cannot change" />
      <TextField label="Display name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
      <TextField label="Email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
      {notice && <Alert severity={notice.severity}>{notice.text}</Alert>}
      <Box>
        <Button type="submit" variant="contained" disabled={!changed}>Save</Button>
      </Box>
    </Stack>
  )
}

function Password() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [notice, setNotice] = useState<Notice>()
  const mismatch = confirm !== '' && confirm !== next

  const save = async (e: FormEvent) => {
    e.preventDefault()
    if (mismatch) return
    try {
      session.updated(await Identity.updateAccount({ currentPassword: current, newPassword: next }))
      setCurrent('')
      setNext('')
      setConfirm('')
      setNotice({ severity: 'success', text: 'Password changed. You are signed out everywhere else.' })
    } catch (err) {
      setNotice({ severity: 'error', text: (err as Error).message })
    }
  }

  return (
    <Stack component="form" spacing={2} onSubmit={save} sx={{ maxWidth: 420 }}>
      <Typography variant="h6" component="h2">Password</Typography>
      <TextField label="Current password" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
      <TextField label="New password" type="password" autoComplete="new-password" helperText="At least 8 characters" value={next} onChange={(e) => setNext(e.target.value)} />
      <TextField label="Confirm new password" type="password" autoComplete="new-password" error={mismatch} helperText={mismatch ? 'The passwords differ' : undefined} value={confirm} onChange={(e) => setConfirm(e.target.value)} />
      {notice && <Alert severity={notice.severity}>{notice.text}</Alert>}
      <Box>
        <Button type="submit" variant="contained" disabled={!current || !next || mismatch}>Change password</Button>
      </Box>
    </Stack>
  )
}

function Tenants() {
  const { account } = useSession()
  const [error, setError] = useState<string>()
  if (!account) return null
  return (
    <Stack spacing={1} sx={{ maxWidth: 420 }}>
      <Typography variant="h6" component="h2">Tenants</Typography>
      <List dense disablePadding>
        {account.tenants.map((t) => (
          <ListItem
            key={t.id}
            secondaryAction={
              t.id === account.tenantId ? (
                <Chip size="small" label="Current" />
              ) : (
                <Button size="small" onClick={() => session.switchTenant(t.id).catch((err: Error) => setError(err.message))}>
                  Switch
                </Button>
              )
            }
          >
            <ListItemText primary={t.name} />
          </ListItem>
        ))}
      </List>
      {error && <Alert severity="error">{error}</Alert>}
    </Stack>
  )
}

// The account page: the user's profile, password and tenants. In
// single-user mode it offers setting up accounts.
function AccountEditor() {
  const s = useSession()
  const navigate = useNavigate()

  if (s.status === 'singleUser') {
    return (
      <Stack spacing={2} sx={{ p: 3, maxWidth: 520 }}>
        <Typography variant="h6" component="h2">Single-user mode</Typography>
        <Typography variant="body2" color="text.secondary">
          There are no accounts yet, so nobody signs in and everyone is the administrator. Setting up accounts makes the first tenant and its
          administrator, and everything made so far becomes theirs.
        </Typography>
        <Box>
          <Button variant="contained" onClick={() => navigate('/setup?next=/workbench')}>Set up accounts</Button>
        </Box>
      </Stack>
    )
  }
  return (
    <Box sx={{ p: 3, overflow: 'auto', height: '100%', boxSizing: 'border-box' }}>
      <Stack spacing={3} divider={<Divider flexItem />}>
        <Profile />
        <Password />
        <Tenants />
      </Stack>
    </Box>
  )
}

export default AccountEditor
