import { useEffect, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router'
import { Alert, Box, Button, CircularProgress, Link, Paper, Stack, TextField, Typography } from '@mui/material'
import { Identity, type StatusResponse } from '../services/Identity.ts'
import { session } from '../session/session.ts'

// Where to go after signing in: a path on this site only
function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') ? next : '/workbench'
}

// Signs in, or (on /setup, or while there are no accounts) sets up the first
// tenant and administrator, ending single-user mode. Success loads the next
// page afresh, so every view starts with the new session.
function Login({ setup = false }: { setup?: boolean }) {
  const [params] = useSearchParams()
  const next = safeNext(params.get('next'))
  const [status, setStatus] = useState<StatusResponse>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [form, setForm] = useState({ tenantName: '', username: '', displayName: '', email: '', password: '', confirm: '' })
  const field = (name: keyof typeof form) => ({
    value: form[name],
    onChange: (e: { target: { value: string } }) => setForm((f) => ({ ...f, [name]: e.target.value })),
  })

  useEffect(() => {
    Identity.status()
      .then(setStatus)
      .catch((err: Error) => setError(err.message))
  }, [])

  const settingUp = status?.singleUser ?? setup
  const mismatch = settingUp && form.confirm !== '' && form.confirm !== form.password

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (mismatch) return
    setBusy(true)
    setError(undefined)
    try {
      const resp = settingUp
        ? await Identity.setup({ tenantName: form.tenantName, username: form.username, displayName: form.displayName, email: form.email, password: form.password })
        : await Identity.login({ username: form.username, password: form.password })
      session.started(resp)
      location.assign(next)
    } catch (err) {
      setError((err as Error).message)
      setBusy(false)
    }
  }

  if (!status && !error) {
    return (
      <Box sx={{ display: 'grid', placeItems: 'center', height: '100vh' }}>
        <CircularProgress aria-label="Loading" />
      </Box>
    )
  }

  return (
    <Box component="main" sx={{ display: 'grid', placeItems: 'center', minHeight: '100vh', p: 2, bgcolor: 'background.default' }}>
      <Paper variant="outlined" sx={{ width: '100%', maxWidth: 400, p: 3 }}>
        <Stack component="form" spacing={2} onSubmit={submit} noValidate>
          <Typography variant="h5" component="h1">
            {settingUp ? 'Set up accounts' : 'Sign in'}
          </Typography>
          {settingUp && (
            <Typography variant="body2" color="text.secondary">
              Foundation runs without sign-in until accounts exist. This makes the first tenant and its administrator; everything made
              so far becomes theirs, and from then on everyone signs in.
            </Typography>
          )}
          {settingUp && <TextField label="Tenant name" required autoFocus {...field('tenantName')} />}
          <TextField label="Username" required autoFocus={!settingUp} autoComplete="username" {...field('username')} />
          {settingUp && <TextField label="Display name" {...field('displayName')} />}
          {settingUp && <TextField label="Email" type="email" autoComplete="email" {...field('email')} />}
          <TextField
            label="Password"
            type="password"
            required
            autoComplete={settingUp ? 'new-password' : 'current-password'}
            helperText={settingUp ? 'At least 8 characters' : undefined}
            {...field('password')}
          />
          {settingUp && (
            <TextField
              label="Confirm password"
              type="password"
              required
              autoComplete="new-password"
              error={mismatch}
              helperText={mismatch ? 'The passwords differ' : undefined}
              {...field('confirm')}
            />
          )}
          {error && <Alert severity="error">{error}</Alert>}
          <Button type="submit" variant="contained" disabled={busy || mismatch}>
            {settingUp ? 'Set up and sign in' : 'Sign in'}
          </Button>
          {status?.singleUser && (
            <Link href={next} variant="body2" sx={{ textAlign: 'center' }}>
              Continue without accounts
            </Link>
          )}
        </Stack>
      </Paper>
    </Box>
  )
}

export default Login
