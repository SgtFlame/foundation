// The Share dialog (documentation/RBAC.md › The Role Editor): who
// holds which role on one resource (a Volume, the tenant, the
// application), granting a role to a member with an optional expiry, and
// revoking. Grants on the resource's ancestors apply too and are listed
// below, read-only.

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  List,
  ListItem,
  ListItemText,
  MenuItem,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import DeleteIcon from '@mui/icons-material/Delete'
import { Authz, type Grant, type Role } from '../../services/Authz.ts'
import { ancestors, describeResource, grantable, peopleFor, type Person } from './grants.ts'
import { permissions, useCan } from '../../session/permissions.ts'
import { useSession } from '../../session/session.ts'

function GrantRow({ grant, role, person, onRevoke, mayRevoke }: { grant: Grant; role?: Role; person?: Person; onRevoke?: () => void; mayRevoke: boolean }) {
  const expiry = grant.expiresAt ? ` · until ${new Date(grant.expiresAt).toLocaleDateString()}` : ''
  return (
    <ListItem
      secondaryAction={
        onRevoke && (
          <Tooltip title={mayRevoke ? 'Revoke' : 'You cannot revoke this'}>
            <span>
              <IconButton edge="end" aria-label={`Revoke ${role?.name ?? 'role'} from ${person?.name ?? grant.userId}`} disabled={!mayRevoke} onClick={onRevoke}>
                <DeleteIcon fontSize="small" />
              </IconButton>
            </span>
          </Tooltip>
        )
      }
    >
      <ListItemText primary={person?.name ?? `User ${grant.userId}`} secondary={`${role?.name ?? `Role ${grant.roleId}`}${expiry}`} />
    </ListItem>
  )
}

function ShareDialog({ resource, title, onClose }: { resource: string; title?: string; onClose: () => void }) {
  const s = useSession()
  const can = useCan()
  const tenantId = s.account?.tenantId ?? '1'
  const [grants, setGrants] = useState<Grant[]>([])
  const [inherited, setInherited] = useState<Grant[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [people, setPeople] = useState<Person[]>([])
  const [user, setUser] = useState('')
  const [role, setRole] = useState('')
  const [expires, setExpires] = useState('')
  const [error, setError] = useState<string>()
  const fail = useCallback((err: unknown) => setError((err as Error).message), [])
  const mayGrant = can('grant.write', resource)

  const load = useCallback(() => {
    Authz.grants({ resource }).then((r) => setGrants(r.grants), fail)
    Promise.all(ancestors(resource).map((a) => Authz.grants({ resource: a }).then((r) => r.grants, () => [] as Grant[])))
      .then((lists) => setInherited(lists.flat()), fail)
  }, [resource, fail])
  useEffect(load, [load])
  useEffect(() => {
    Authz.roles().then((r) => setRoles(r.roles), fail)
    peopleFor(resource, tenantId).then(setPeople, fail)
  }, [resource, tenantId, fail])

  const roleById = useMemo(() => new Map(roles.map((r) => [r.id, r])), [roles])
  const personById = useMemo(() => new Map(people.map((p) => [p.id, p])), [people])
  const offered = roles.filter((r) => grantable(r, resource))

  const run = async (op: () => Promise<unknown>) => {
    setError(undefined)
    try {
      await op()
      load()
      void permissions.load() // your own access may have changed
    } catch (err) {
      fail(err)
    }
  }

  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>Share {title ?? describeResource(resource)}</DialogTitle>
      <DialogContent>
        <Stack spacing={2}>
          <Typography variant="body2" color="text.secondary">
            A role here applies to {title ?? describeResource(resource)} and everything in it.
          </Typography>
          <List dense disablePadding aria-label="Grants here">
            {grants.map((g) => (
              <GrantRow key={g.id} grant={g} role={roleById.get(g.roleId)} person={personById.get(g.userId)} mayRevoke={mayGrant} onRevoke={() => void run(() => Authz.revoke(g.id))} />
            ))}
            {grants.length === 0 && (
              <Typography variant="body2" color="text.secondary">
                Nobody has a role on it directly.
              </Typography>
            )}
          </List>
          {mayGrant && (
            <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap', rowGap: 1 }}>
              <TextField select size="small" label="Person" value={user} onChange={(e) => setUser(e.target.value)} sx={{ minWidth: 180 }}>
                {people.map((p) => (
                  <MenuItem key={p.id} value={p.id}>
                    {p.name}
                  </MenuItem>
                ))}
              </TextField>
              <TextField select size="small" label="Role" value={role} onChange={(e) => setRole(e.target.value)} sx={{ minWidth: 160 }}>
                {offered.map((r) => (
                  <MenuItem key={r.id} value={r.id}>
                    {r.name}
                  </MenuItem>
                ))}
              </TextField>
              <TextField
                size="small"
                type="date"
                label="Until (optional)"
                value={expires}
                onChange={(e) => setExpires(e.target.value)}
                slotProps={{ inputLabel: { shrink: true } }}
              />
              <Button
                variant="contained"
                disabled={!user || !role}
                onClick={() =>
                  void run(async () => {
                    await Authz.grant({ userId: user, roleId: role, resource, expiresAt: expires ? new Date(`${expires}T23:59:59`).toISOString() : undefined })
                    setUser('')
                    setRole('')
                    setExpires('')
                  })
                }
              >
                Grant
              </Button>
            </Stack>
          )}
          {inherited.length > 0 && (
            <>
              <Typography variant="subtitle2" component="h3">
                Also applying, from above
              </Typography>
              <List dense disablePadding aria-label="Inherited grants">
                {inherited.map((g) => (
                  <GrantRow key={g.id} grant={g} role={roleById.get(g.roleId)} person={personById.get(g.userId)} mayRevoke={false} />
                ))}
              </List>
            </>
          )}
          {error && <Alert severity="error">{error}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Done</Button>
      </DialogActions>
    </Dialog>
  )
}

export default ShareDialog
