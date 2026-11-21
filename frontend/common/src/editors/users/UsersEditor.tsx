import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Checkbox,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  IconButton,
  ListItemText,
  MenuItem,
  Select,
  Stack,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import EditIcon from '@mui/icons-material/Edit'
import ToolbarAction from '../../components/folders/ToolbarAction.tsx'
import { Logs, type UserLevel } from '../../services/Logs.ts'
import { Tenants, type TenantSummary } from '../../services/Tenants.ts'
import { Users, type UserSummary } from '../../services/Users.ts'
import { useSession } from '../../session/session.ts'

interface Draft {
  username: string
  displayName: string
  email: string
  password: string
  superAdmin: boolean
  disabled: boolean
  tenantIds: string[]
}

const empty: Draft = { username: '', displayName: '', email: '', password: '', superAdmin: false, disabled: false, tenantIds: [] }

// Creates a user, or edits one: their profile, flags and a new password.
// Tenants are chosen when creating; afterwards the Tenant editor does it.
function UserDialog({
  user,
  tenants,
  self,
  onClose,
  onSaved,
}: {
  user?: UserSummary
  tenants: TenantSummary[]
  self: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const [draft, setDraft] = useState<Draft>(
    user ? { ...empty, username: user.username, displayName: user.displayName, email: user.email ?? '', superAdmin: user.superAdmin, disabled: !!user.disabled, tenantIds: user.tenantIds } : empty,
  )
  const [error, setError] = useState<string>()
  const set = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }))
  const valid = user ? true : draft.username.trim() !== '' && draft.password !== '' && draft.tenantIds.length > 0

  const save = async () => {
    try {
      if (user) {
        await Users.update({
          userId: user.id,
          displayName: draft.displayName,
          email: draft.email,
          superAdmin: draft.superAdmin,
          disabled: draft.disabled,
          password: draft.password || undefined,
        })
      } else {
        await Users.create({ ...draft })
      }
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>{user ? `Edit ${user.username}` : 'New user'}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <TextField label="Username" value={draft.username} disabled={!!user} autoFocus={!user} onChange={(e) => set({ username: e.target.value })} />
          <TextField label="Display name" value={draft.displayName} onChange={(e) => set({ displayName: e.target.value })} />
          <TextField label="Email" type="email" value={draft.email} onChange={(e) => set({ email: e.target.value })} />
          <TextField
            label={user ? 'New password' : 'Password'}
            type="password"
            autoComplete="new-password"
            helperText={user ? 'Leave empty to keep it. A new one signs them out.' : 'At least 8 characters; they can change it on their Account page'}
            value={draft.password}
            onChange={(e) => set({ password: e.target.value })}
          />
          {!user && (
            <Select
              multiple
              displayEmpty
              value={draft.tenantIds}
              onChange={(e) => set({ tenantIds: typeof e.target.value === 'string' ? e.target.value.split(',') : e.target.value })}
              renderValue={(ids) => (ids.length ? ids.map((id) => tenants.find((t) => t.id === id)?.name ?? id).join(', ') : 'Tenants…')}
              inputProps={{ 'aria-label': 'Tenants' }}
            >
              {tenants.map((t) => (
                <MenuItem key={t.id} value={t.id}>
                  <Checkbox checked={draft.tenantIds.includes(t.id)} size="small" />
                  <ListItemText primary={t.name} />
                </MenuItem>
              ))}
            </Select>
          )}
          <FormControlLabel
            control={<Checkbox checked={draft.superAdmin} disabled={self} onChange={(e) => set({ superAdmin: e.target.checked })} />}
            label="Super Administrator: everything, everywhere"
          />
          {user && (
            <FormControlLabel
              control={<Checkbox checked={draft.disabled} disabled={self} onChange={(e) => set({ disabled: e.target.checked })} />}
              label="Disabled: cannot sign in"
            />
          )}
          {error && <Alert severity="error">{error}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button onClick={() => void save()} disabled={!valid}>
          {user ? 'Save' : 'Create'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// Administration › Users: every user with their flags and tenants, and
// full logging for one user (debug for 24 hours).
function UsersEditor() {
  const s = useSession()
  const [users, setUsers] = useState<UserSummary[]>([])
  const [tenants, setTenants] = useState<TenantSummary[]>([])
  const [levels, setLevels] = useState<UserLevel[]>([])
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<UserSummary | 'new'>()
  const fail = useCallback((err: unknown) => setError((err as Error).message), [])

  const load = useCallback(() => {
    Users.list().then((r) => setUsers(r.users), fail)
    Tenants.list().then((r) => setTenants(r.tenants), fail)
    Logs.userLevels().then((r) => setLevels(r.levels), fail)
  }, [fail])
  useEffect(load, [load])

  const fullLogging = async (user: UserSummary, on: boolean) => {
    try {
      await Logs.setUserLevel({ userId: user.id, level: on ? 'debug' : '' })
      load()
    } catch (err) {
      fail(err)
    }
  }
  const myId = s.account?.user.id ?? '1'

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Stack direction="row" sx={{ px: 1, borderBottom: 1, borderColor: 'divider' }}>
        <ToolbarAction title="New user" onClick={() => setEditing('new')}>
          <AddIcon />
        </ToolbarAction>
      </Stack>
      {error && (
        <Alert severity="error" onClose={() => setError(undefined)} sx={{ m: 1 }}>
          {error}
        </Alert>
      )}
      <TableContainer sx={{ flex: 1, minHeight: 0 }}>
        <Table size="small" stickyHeader aria-label="Users">
          <TableHead>
            <TableRow>
              <TableCell>User</TableCell>
              <TableCell>Email</TableCell>
              <TableCell>Tenants</TableCell>
              <TableCell>Status</TableCell>
              <TableCell>Full logging</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {users.map((u) => {
              const level = levels.find((l) => l.userId === u.id)
              return (
                <TableRow key={u.id} hover>
                  <TableCell>
                    <ListItemText primary={u.displayName} secondary={u.username} sx={{ my: 0 }} />
                  </TableCell>
                  <TableCell>{u.email}</TableCell>
                  <TableCell>
                    <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', gap: 0.5 }}>
                      {u.tenantIds.map((id) => (
                        <Chip key={id} size="small" label={tenants.find((t) => t.id === id)?.name ?? id} />
                      ))}
                    </Stack>
                  </TableCell>
                  <TableCell>
                    <Stack direction="row" spacing={0.5}>
                      {u.superAdmin && <Chip size="small" color="primary" label="Super Administrator" />}
                      {u.disabled && <Chip size="small" color="warning" label="Disabled" />}
                    </Stack>
                  </TableCell>
                  <TableCell>
                    <Tooltip title={level ? `Debug until ${new Date(level.expiresAt).toLocaleString()}` : 'Store this user\'s debug records for 24 hours'}>
                      <Switch
                        size="small"
                        checked={!!level}
                        onChange={(e) => void fullLogging(u, e.target.checked)}
                        slotProps={{ input: { 'aria-label': `Full logging for ${u.username}` } }}
                      />
                    </Tooltip>
                  </TableCell>
                  <TableCell align="right">
                    <IconButton size="small" aria-label={`Edit ${u.username}`} onClick={() => setEditing(u)}>
                      <EditIcon fontSize="small" />
                    </IconButton>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </TableContainer>
      {editing && (
        <UserDialog
          user={editing === 'new' ? undefined : editing}
          tenants={tenants}
          self={editing !== 'new' && editing.id === myId}
          onClose={() => setEditing(undefined)}
          onSaved={() => {
            setEditing(undefined)
            load()
          }}
        />
      )}
    </Stack>
  )
}

export default UsersEditor
