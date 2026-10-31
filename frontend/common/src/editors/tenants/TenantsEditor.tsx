import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  MenuItem,
  Select,
  Stack,
  Tooltip,
  Typography,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import DeleteIcon from '@mui/icons-material/Delete'
import EditIcon from '@mui/icons-material/Edit'
import PersonRemoveIcon from '@mui/icons-material/PersonRemove'
import NameDialog from '../../components/folders/NameDialog.tsx'
import ToolbarAction from '../../components/folders/ToolbarAction.tsx'
import ShareDialog from '../../components/share/ShareDialog.tsx'
import { Tenants, type Member, type TenantSummary } from '../../services/Tenants.ts'
import { Users, type UserSummary } from '../../services/Users.ts'
import { useSession } from '../../session/session.ts'
import { useCan } from '../../session/permissions.ts'

const defaultTenant = '1'

// The selected tenant's members with their roles there, with adding (as
// Editor) and removing; roles are changed in the Share dialog, from within
// the tenant
function Members({ tenant, users, current, onChange, fail }: { tenant: TenantSummary; users: UserSummary[]; current: boolean; onChange: () => void; fail: (err: unknown) => void }) {
  const can = useCan()
  const [members, setMembers] = useState<Member[]>([])
  const [adding, setAdding] = useState('')
  const [sharing, setSharing] = useState(false)
  const resource = `tenant:${tenant.id}`

  const load = useCallback(() => {
    Tenants.members(tenant.id).then((r) => setMembers(r.users), fail)
  }, [tenant.id, fail])
  useEffect(load, [load])

  const run = async (op: () => Promise<unknown>) => {
    try {
      await op()
      load()
      onChange()
    } catch (err) {
      fail(err)
    }
  }
  const others = users.filter((u) => !members.some((m) => m.id === u.id))

  return (
    <Stack spacing={1} sx={{ minWidth: 0 }}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <Typography variant="subtitle1" component="h3" sx={{ flex: 1 }}>
          Members of {tenant.name}
        </Typography>
        <Tooltip title={current ? 'Grant and revoke roles in this tenant' : 'Switch to this tenant to change its roles'}>
          <span>
            <Button size="small" disabled={!current || !can('grant.read', resource)} onClick={() => setSharing(true)}>
              Roles…
            </Button>
          </span>
        </Tooltip>
      </Stack>
      <List dense disablePadding>
        {members.map((m) => (
          <ListItem
            key={m.id}
            secondaryAction={
              <Tooltip title="Remove from the tenant">
                <IconButton edge="end" aria-label={`Remove ${m.username}`} onClick={() => void run(() => Tenants.removeMember(tenant.id, m.id))}>
                  <PersonRemoveIcon fontSize="small" />
                </IconButton>
              </Tooltip>
            }
          >
            <ListItemText
              primary={m.displayName}
              secondary={`${m.username} · ${m.roles.length ? m.roles.join(', ') : 'no role'}${m.superAdmin ? ' · Super Administrator' : ''}${m.disabled ? ' · disabled' : ''}`}
            />
          </ListItem>
        ))}
        {members.length === 0 && <Typography variant="body2" color="text.secondary">No members</Typography>}
      </List>
      {sharing && (
        <ShareDialog
          resource={resource}
          title={tenant.name}
          onClose={() => {
            setSharing(false)
            load()
          }}
        />
      )}
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', display: users.length ? undefined : 'none' }}>
        <Select size="small" displayEmpty value={adding} onChange={(e) => setAdding(e.target.value)} sx={{ minWidth: 200 }} inputProps={{ 'aria-label': 'User to add' }}>
          <MenuItem value="">
            <em>Add a user…</em>
          </MenuItem>
          {others.map((u) => (
            <MenuItem key={u.id} value={u.id}>
              {u.displayName} ({u.username})
            </MenuItem>
          ))}
        </Select>
        <Button
          startIcon={<AddIcon />}
          disabled={!adding}
          onClick={() => void run(async () => {
            await Tenants.addMember(tenant.id, adding)
            setAdding('')
          })}
        >
          Add
        </Button>
      </Stack>
    </Stack>
  )
}

// Administration › Tenants: create, rename and delete tenants, and choose
// their members. A tenant's content is not deleted with it yet. A tenant's
// Owner or Administrator sees only their tenant, and adds no one: adding
// needs the users list (user.list).
function TenantsEditor() {
  const s = useSession()
  const can = useCan()
  const mayListUsers = can('user.list', 'app')
  const [tenants, setTenants] = useState<TenantSummary[]>([])
  const [users, setUsers] = useState<UserSummary[]>([])
  const [selected, setSelected] = useState<string>()
  const [error, setError] = useState<string>()
  const [creating, setCreating] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const fail = useCallback((err: unknown) => setError((err as Error).message), [])

  const load = useCallback(() => {
    Tenants.list().then((r) => setTenants(r.tenants), fail)
    if (mayListUsers) Users.list().then((r) => setUsers(r.users), fail)
  }, [fail, mayListUsers])
  useEffect(load, [load])

  const tenant = tenants.find((t) => t.id === selected)
  const run = async (op: () => Promise<unknown>) => {
    setError(undefined)
    try {
      await op()
      load()
    } catch (err) {
      fail(err)
    }
  }

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Stack direction="row" sx={{ px: 1, borderBottom: 1, borderColor: 'divider', alignItems: 'center' }}>
        <ToolbarAction title="New tenant" disabled={!can('tenant.create', 'app')} onClick={() => setCreating(true)}>
          <AddIcon />
        </ToolbarAction>
        <ToolbarAction title="Rename tenant" disabled={!tenant} onClick={() => setRenaming(true)}>
          <EditIcon />
        </ToolbarAction>
        <ToolbarAction title={tenant?.id === defaultTenant ? 'The default tenant cannot be deleted' : 'Delete tenant'} disabled={!tenant || tenant.id === defaultTenant} onClick={() => setDeleting(true)}>
          <DeleteIcon />
        </ToolbarAction>
      </Stack>
      {error && (
        <Alert severity="error" onClose={() => setError(undefined)} sx={{ m: 1 }}>
          {error}
        </Alert>
      )}
      <Stack direction="row" spacing={2} sx={{ flex: 1, minHeight: 0, p: 1, overflow: 'auto' }}>
        <List dense sx={{ width: 280, flexShrink: 0 }} aria-label="Tenants">
          {tenants.map((t) => (
            <ListItemButton key={t.id} selected={t.id === selected} onClick={() => setSelected(t.id)}>
              <ListItemText
                primary={t.name + (t.id === s.account?.tenantId ? ' (current)' : '')}
                secondary={`${t.members} member${t.members === 1 ? '' : 's'}${t.id === defaultTenant ? ' · default, has the Volumes' : ''}`}
              />
            </ListItemButton>
          ))}
        </List>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          {tenant ? (
            <Members tenant={tenant} users={users} current={tenant.id === (s.account?.tenantId ?? defaultTenant)} onChange={load} fail={fail} />
          ) : (
            <Typography variant="body2" color="text.secondary" sx={{ p: 1 }}>
              Select a tenant to see its members.
            </Typography>
          )}
        </Box>
      </Stack>
      {creating && <NameDialog open title="New tenant" onClose={() => setCreating(false)} onCreate={(name) => { setCreating(false); void run(() => Tenants.create(name)) }} />}
      {renaming && tenant && (
        <NameDialog open title={`Rename ${tenant.name}`} onClose={() => setRenaming(false)} onCreate={(name) => { setRenaming(false); void run(() => Tenants.rename(tenant.id, name)) }} />
      )}
      <Dialog open={deleting && !!tenant} onClose={() => setDeleting(false)}>
        <DialogTitle>Delete {tenant?.name}?</DialogTitle>
        <DialogContent>
          <Typography variant="body2">
            Its members lose access and are signed out of it. What was made in it is kept for now, but nobody can reach it.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleting(false)}>Cancel</Button>
          <Button
            color="error"
            onClick={() => {
              setDeleting(false)
              setSelected(undefined)
              if (tenant) void run(() => Tenants.delete(tenant.id))
            }}
          >
            Delete
          </Button>
        </DialogActions>
      </Dialog>
    </Stack>
  )
}

export default TenantsEditor
