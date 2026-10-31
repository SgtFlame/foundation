// The Secrets editor (documentation/Secrets.md › The Secrets editor):
// the server's secrets in Vault and the app scope's secret settings, for
// Application Administrators. It is write-only: it shows whether each
// secret is set, its version and who changed it last, never a value. A new
// value is typed into a password field that is cleared when its dialog
// closes.

import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import RefreshIcon from '@mui/icons-material/Refresh'
import ToolbarAction from '../../components/folders/ToolbarAction.tsx'
import { log } from '../../logging/index.ts'
import { useCan } from '../../session/permissions.ts'
import { Secrets, type Secret, type StatusResponse } from '../../services/Secrets.ts'

const secretsLog = log.In('secrets')

const serverName = /^server(\/[a-z0-9][a-z0-9._-]{0,63}){1,4}$/

// What a dialog is doing: setting an existing secret, making a new server
// secret, or deleting one
type Editing = { kind: 'set'; secret: Secret } | { kind: 'new' } | { kind: 'delete'; secret: Secret }

function SetDialog({ editing, onClose, onDone }: { editing: Editing & { kind: 'set' | 'new' }; onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState('server/')
  const [value, setValue] = useState('')
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const target = editing.kind === 'set' ? editing.secret.name : name
  const badName = editing.kind === 'new' && !serverName.test(name)

  const save = async () => {
    setBusy(true)
    setError(undefined)
    try {
      await Secrets.put(target, value)
      secretsLog.With('secret', target).info('set %s', target)
      setValue('')
      onDone()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    // The value lives only in this dialog's state, which closing unmounts
    <Dialog open onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>{editing.kind === 'set' ? `Set ${editing.secret.title ?? editing.secret.name}` : 'New server secret'}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          {editing.kind === 'new' ? (
            <TextField
              size="small"
              label="Name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              error={name !== 'server/' && badName}
              helperText="server/<name>, in lower case, e.g. server/smtp-password"
              autoFocus
            />
          ) : (
            <Typography variant="body2" color="text.secondary" sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>
              {editing.secret.name}
            </Typography>
          )}
          <TextField
            size="small"
            type="password"
            label="New value"
            autoComplete="new-password"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && value && !badName && !busy && void save()}
            helperText={editing.kind === 'set' && editing.secret.set ? 'Replaces the current value, which Vault keeps as an earlier version' : undefined}
            autoFocus={editing.kind === 'set'}
          />
          {error && <Alert severity="error">{error}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" disabled={!value || badName || busy} onClick={() => void save()}>
          Save
        </Button>
      </DialogActions>
    </Dialog>
  )
}

function DeleteDialog({ secret, onClose, onDone }: { secret: Secret; onClose: () => void; onDone: () => void }) {
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const remove = async () => {
    setBusy(true)
    try {
      await Secrets.delete(secret.name)
      secretsLog.With('secret', secret.name).info('deleted %s', secret.name)
      onDone()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open onClose={onClose}>
      <DialogTitle>Delete {secret.title ?? secret.name}?</DialogTitle>
      <DialogContent>
        <DialogContentText>
          {secret.kind === 'server'
            ? 'Vault keeps its earlier versions, so an operator can bring it back. The server reads it when it unseals, so it keeps the current value until it restarts.'
            : 'The setting is cleared at the app scope, at once. Tenants and users who set their own keep them.'}
        </DialogContentText>
        {error && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {error}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button color="error" variant="contained" disabled={busy} onClick={() => void remove()}>
          Delete
        </Button>
      </DialogActions>
    </Dialog>
  )
}

function StatusLine({ status }: { status?: StatusResponse }) {
  if (!status) return null
  if (!status.vault) {
    return (
      <Typography variant="body2" color="text.secondary">
        No Vault: secret settings are sealed in the database with the key file
      </Typography>
    )
  }
  const expires = status.tokenExpires && new Date(status.tokenExpires)
  return (
    <Stack direction="row" spacing={1} sx={{ alignItems: 'center', minWidth: 0 }}>
      <Chip size="small" color={status.state === 'ready' ? 'success' : 'warning'} variant="outlined" label={status.state} />
      <Typography variant="body2" color="text.secondary" noWrap>
        Vault at {status.addr}, mount {status.mount}
        {expires && ` · token valid until ${expires.toLocaleTimeString()}`}
      </Typography>
    </Stack>
  )
}

// A name that wraps only after its / and . separators
function Breakable({ name }: { name: string }) {
  return (
    <>
      {name.split(/(?<=[/.])/).map((part, i) => (
        <span key={i}>
          {part}
          <wbr />
        </span>
      ))}
    </>
  )
}

function SecretRow({ secret, writable, edit }: { secret: Secret; writable: boolean; edit: (e: Editing) => void }) {
  const updated = secret.updatedAt && new Date(secret.updatedAt)
  return (
    <TableRow hover>
      <TableCell sx={{ minWidth: 200, maxWidth: 320 }}>
        <Typography variant="body2">{secret.title ?? secret.name.replace(/^server\//, '')}</Typography>
        <Typography variant="caption" color="text.secondary" sx={{ fontFamily: 'monospace' }}>
          <Breakable name={secret.name} />
        </Typography>
      </TableCell>
      <TableCell>
        <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', rowGap: 0.5 }}>
          <Chip size="small" label={secret.set ? 'Set' : 'Not set'} color={secret.set ? 'primary' : 'default'} variant={secret.set ? 'filled' : 'outlined'} />
          {secret.source === 'env' && (
            <Tooltip title="A flag or environment variable sets this, and wins over a stored value">
              <Chip size="small" variant="outlined" label="environment wins" />
            </Tooltip>
          )}
          {secret.source === 'server' && (
            <Tooltip title="Not set here: the server's own key from Vault is used">
              <Chip size="small" variant="outlined" label="server key used" />
            </Tooltip>
          )}
        </Stack>
      </TableCell>
      <TableCell align="right">{secret.version || '–'}</TableCell>
      <TableCell>
        <Typography variant="body2" noWrap title={updated ? updated.toLocaleString() : undefined}>
          {updated ? updated.toLocaleDateString() : '–'}
        </Typography>
        {secret.updatedBy && (
          <Typography variant="caption" color="text.secondary" noWrap>
            {secret.updatedBy}
          </Typography>
        )}
      </TableCell>
      <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
        {secret.protected ? (
          <Tooltip title="Rotated with backend/scripts/secrets/rotate-db-key.sh">
            <Chip size="small" variant="outlined" label="rotate by script" />
          </Tooltip>
        ) : (
          writable && (
            <>
              <Button size="small" onClick={() => edit({ kind: 'set', secret })}>
                Set…
              </Button>
              <Button size="small" color="error" disabled={!secret.set} onClick={() => edit({ kind: 'delete', secret })}>
                Delete
              </Button>
            </>
          )
        )}
      </TableCell>
    </TableRow>
  )
}

function Group({ title, note, secrets, writable, edit }: { title: string; note: string; secrets: Secret[]; writable: boolean; edit: (e: Editing) => void }) {
  return (
    <Box sx={{ mb: 3 }}>
      <Typography variant="subtitle2" component="h3">
        {title}
      </Typography>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
        {note}
      </Typography>
      <Box sx={{ overflowX: 'auto' }}>
        <Table size="small" sx={{ minWidth: 640 }}>
          <TableHead>
            <TableRow>
              <TableCell>Secret</TableCell>
              <TableCell>Status</TableCell>
              <TableCell align="right">Version</TableCell>
              <TableCell>Changed</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {secrets.map((s) => (
              <SecretRow key={s.name} secret={s} writable={writable} edit={edit} />
            ))}
          </TableBody>
        </Table>
      </Box>
    </Box>
  )
}

function SecretsEditor() {
  const can = useCan()
  const writable = can('secret.write', 'app')
  const [secrets, setSecrets] = useState<Secret[]>([])
  const [status, setStatus] = useState<StatusResponse>()
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Editing>()
  const fail = useCallback((err: unknown) => setError((err as Error).message), [])
  const load = useCallback(() => {
    Secrets.status().then(setStatus, fail)
    Secrets.list().then((r) => {
      setSecrets(r.secrets)
      setError(undefined)
    }, fail)
  }, [fail])
  useEffect(load, [load])
  const done = () => {
    setEditing(undefined)
    load()
  }

  const server = secrets.filter((s) => s.kind === 'server')
  const settings = secrets.filter((s) => s.kind === 'setting')

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Stack direction="row" spacing={1} sx={{ px: 1, borderBottom: 1, borderColor: 'divider', alignItems: 'center', minHeight: 40 }}>
        <ToolbarAction title="Refresh" onClick={load}>
          <RefreshIcon />
        </ToolbarAction>
        <ToolbarAction title={status?.vault ? 'New server secret' : 'Server secrets need Vault'} disabled={!writable || !status?.vault} onClick={() => setEditing({ kind: 'new' })}>
          <AddIcon />
        </ToolbarAction>
        <StatusLine status={status} />
      </Stack>
      {status?.state === 'degraded' && (
        <Alert severity="warning" sx={{ m: 1 }}>
          The server is running on cached secrets: {status.problem}. It recovers by itself once Vault answers; if its token has expired, unseal it again with{' '}
          <code>unseal.sh</code>.
        </Alert>
      )}
      {error && (
        <Alert severity="error" onClose={() => setError(undefined)} sx={{ m: 1 }}>
          {error}
        </Alert>
      )}
      <Box sx={{ p: 1.5, overflow: 'auto', flex: 1, maxWidth: 1000 }}>
        {status?.vault && (
          <Group
            title="Server secrets"
            note="The bootstrap bundle in Vault (server/…), read when the server unseals: a change applies at the next start. server/gemini-api-key is the server's Gemini key, under any key set in Settings."
            secrets={server}
            writable={writable}
            edit={setEditing}
          />
        )}
        <Group
          title="Secret settings at the app scope"
          note="The same fields as in Settings, at the app scope, applied at once. Tenants and users may set their own, which win for their calls."
          secrets={settings}
          writable={writable}
          edit={setEditing}
        />
        {!writable && (
          <Typography variant="body2" color="text.secondary">
            You may see which secrets are set; only an Application Administrator changes them.
          </Typography>
        )}
      </Box>
      {editing?.kind === 'delete' && <DeleteDialog secret={editing.secret} onClose={() => setEditing(undefined)} onDone={done} />}
      {(editing?.kind === 'set' || editing?.kind === 'new') && <SetDialog editing={editing} onClose={() => setEditing(undefined)} onDone={done} />}
    </Stack>
  )
}

export default SecretsEditor
