// The Role Editor (documentation/RBAC.md › The Role Editor): the roles
// of this tenant (built-in ones read-only), each with its permission matrix
// and who holds it where, and an access checker. The matrix is built from
// the server's resource types: a row per type, grouped by feature, a column
// per class of action, and a checkbox per action the type declares.

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Checkbox,
  Chip,
  IconButton,
  List,
  ListItemButton,
  ListItemText,
  ListSubheader,
  MenuItem,
  Stack,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tabs,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import DeleteIcon from '@mui/icons-material/Delete'
import SaveIcon from '@mui/icons-material/Save'
import ToolbarAction from '../../components/folders/ToolbarAction.tsx'
import { grantable, peopleFor, type Person } from '../../components/share/grants.ts'
import { Authz, type ActionClass, type Decision, type Grant, type ResourceType, type Role } from '../../services/Authz.ts'
import { permissions, tenantOf, useCan } from '../../session/permissions.ts'
import { useSession } from '../../session/session.ts'
import ResourcePicker from './ResourcePicker.tsx'

const classes: ActionClass[] = ['read', 'write', 'create', 'delete', 'execute', 'share', 'moderate', 'admin']

interface Draft {
  id?: string
  name: string
  description: string
  permissions: Set<string>
  revision: number
}

const draftOf = (r: Role): Draft => ({ id: r.builtIn ? undefined : r.id, name: r.name, description: r.description ?? '', permissions: new Set(r.permissions), revision: r.revision ?? 0 })

// The permission matrix. Read-only for built-in roles. A tenant role (every
// custom role) holds only tenant permissions, so app actions are left out.
function Matrix({ types, draft, scope, readOnly, onChange }: { types: ResourceType[]; draft: Draft; scope: Role['scope']; readOnly: boolean; onChange: (perms: Set<string>) => void }) {
  const [filter, setFilter] = useState('')
  const all = draft.permissions.has('*')
  const shown = types
    .map((t) => ({ ...t, actions: t.actions.filter((a) => a.class !== 'self' && (scope === 'app' || a.on === 'tenant')) }))
    .filter((t) => t.actions.length > 0)
    .filter((t) => !filter || `${t.title} ${t.id} ${t.actions.map((a) => a.title).join(' ')}`.toLowerCase().includes(filter.toLowerCase()))
  const bySource = new Map<string, typeof shown>()
  for (const t of shown) bySource.set(t.source, [...(bySource.get(t.source) ?? []), t])
  const has = (p: string) => all || draft.permissions.has(p)
  const toggle = (perms: string[], on: boolean) => {
    const next = new Set(draft.permissions)
    for (const p of perms) {
      if (on) next.add(p)
      else next.delete(p)
    }
    onChange(next)
  }
  const column = (c: ActionClass) => shown.flatMap((t) => t.actions.filter((a) => a.class === c).map((a) => `${t.id}.${a.name}`))

  return (
    <Stack spacing={1} sx={{ minWidth: 0 }}>
      <TextField size="small" placeholder="Filter resource types" value={filter} onChange={(e) => setFilter(e.target.value)} sx={{ maxWidth: 320 }} />
      {all && <Alert severity="info">This role holds every permission, including those of features added later.</Alert>}
      <Box sx={{ overflow: 'auto' }}>
        <Table size="small" stickyHeader aria-label="Permissions" sx={{ '& th, & td': { px: 0.5 }, '& th': { fontSize: 13 } }}>
          <TableHead>
            <TableRow>
              <TableCell>Resource type</TableCell>
              {classes.map((c) => {
                const perms = column(c)
                const on = perms.length > 0 && perms.every(has)
                return (
                  <TableCell key={c} align="center" sx={{ textTransform: 'capitalize' }}>
                    {c}
                    {!readOnly && perms.length > 0 && (
                      <Checkbox size="small" checked={on} onChange={(e) => toggle(perms, e.target.checked)} slotProps={{ input: { 'aria-label': `Every ${c} permission` } }} />
                    )}
                  </TableCell>
                )
              })}
            </TableRow>
          </TableHead>
          <TableBody>
            {[...bySource].map(([source, list]) => [
              <TableRow key={`source-${source}`}>
                <TableCell colSpan={classes.length + 1} sx={{ bgcolor: 'action.hover', fontWeight: 600, py: 0.5 }}>
                  {source}
                </TableCell>
              </TableRow>,
              ...list.map((t) => {
                const row = t.actions.map((a) => `${t.id}.${a.name}`)
                return (
                  <TableRow key={t.id} hover>
                    <TableCell>
                      <Stack direction="row" sx={{ alignItems: 'center' }}>
                        {!readOnly && <Checkbox size="small" checked={row.every(has)} onChange={(e) => toggle(row, e.target.checked)} slotProps={{ input: { 'aria-label': `Every ${t.title} permission` } }} />}
                        <Box>
                          {t.title}
                          <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                            {t.id}
                          </Typography>
                        </Box>
                      </Stack>
                    </TableCell>
                    {classes.map((c) => (
                      <TableCell key={c} align="center" sx={{ whiteSpace: 'nowrap' }}>
                        {t.actions
                          .filter((a) => a.class === c)
                          .map((a) => {
                            const p = `${t.id}.${a.name}`
                            return (
                              <Tooltip key={p} title={`${a.title} (${p}${a.on === 'app' ? ', held on the application' : ''})`}>
                                <Checkbox size="small" checked={has(p)} disabled={readOnly} onChange={(e) => toggle([p], e.target.checked)} slotProps={{ input: { 'aria-label': p } }} />
                              </Tooltip>
                            )
                          })}
                      </TableCell>
                    ))}
                  </TableRow>
                )
              }),
            ])}
          </TableBody>
        </Table>
      </Box>
    </Stack>
  )
}

// Who holds a role, where, until when; granting it and revoking it
function Grants({ role, people, fail }: { role: Role; people: Person[]; fail: (err: unknown) => void }) {
  const s = useSession()
  const can = useCan()
  const tenant = tenantOf(s)
  const [grants, setGrants] = useState<Grant[]>([])
  const [user, setUser] = useState('')
  const [resource, setResource] = useState(role.scope === 'app' ? 'app' : tenant)
  const [expires, setExpires] = useState('')
  const load = useCallback(() => {
    Authz.grants({ roleId: role.id }).then((r) => setGrants(r.grants), fail)
  }, [role.id, fail])
  useEffect(load, [load])
  const name = (id: string) => people.find((p) => p.id === id)?.name ?? `User ${id}`
  const run = async (op: () => Promise<unknown>) => {
    try {
      await op()
      load()
      void permissions.load()
    } catch (err) {
      fail(err)
    }
  }

  return (
    <Stack spacing={1.5}>
      <List dense disablePadding aria-label={`Holders of ${role.name}`}>
        {grants.map((g) => (
          <ListItemButton key={g.id} disableRipple sx={{ cursor: 'default' }}>
            <ListItemText
              primary={name(g.userId)}
              secondary={`on ${g.resource}${g.expiresAt ? ` · until ${new Date(g.expiresAt).toLocaleDateString()}` : ''} · since ${new Date(g.createdAt).toLocaleDateString()}`}
            />
            <Tooltip title="Revoke">
              <span>
                <IconButton aria-label={`Revoke from ${name(g.userId)}`} disabled={!can('grant.write', g.resource)} onClick={() => void run(() => Authz.revoke(g.id))}>
                  <DeleteIcon fontSize="small" />
                </IconButton>
              </span>
            </Tooltip>
          </ListItemButton>
        ))}
        {grants.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            Nobody holds this role here.
          </Typography>
        )}
      </List>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start', flexWrap: 'wrap', rowGap: 1 }}>
        <TextField select size="small" label="Person" value={user} onChange={(e) => setUser(e.target.value)} sx={{ minWidth: 200 }}>
          {people.map((p) => (
            <MenuItem key={p.id} value={p.id}>
              {p.name}
            </MenuItem>
          ))}
        </TextField>
        {role.scope === 'app' ? (
          <TextField size="small" label="Resource" value="app" disabled />
        ) : (
          <ResourcePicker value={resource} onChange={setResource} tenant={tenant} app={false} />
        )}
        <TextField size="small" type="date" label="Until (optional)" value={expires} onChange={(e) => setExpires(e.target.value)} slotProps={{ inputLabel: { shrink: true } }} />
        <Button
          variant="contained"
          disabled={!user || !resource || !grantable(role, resource) || !can('grant.write', resource)}
          onClick={() =>
            void run(async () => {
              await Authz.grant({ userId: user, roleId: role.id, resource, expiresAt: expires ? new Date(`${expires}T23:59:59`).toISOString() : undefined })
              setUser('')
              setExpires('')
            })
          }
        >
          Grant
        </Button>
      </Stack>
    </Stack>
  )
}

// The access checker: may this person do this here, and why
function AccessChecker({ types, people }: { types: ResourceType[]; people: Person[] }) {
  const s = useSession()
  const can = useCan()
  const tenant = tenantOf(s)
  const [user, setUser] = useState('')
  const [action, setAction] = useState('')
  const [resource, setResource] = useState(tenant)
  const [decision, setDecision] = useState<Decision>()
  const [error, setError] = useState<string>()
  const actions = types.flatMap((t) => t.actions.map((a) => `${t.id}.${a.name}`))

  return (
    <Stack spacing={2} sx={{ p: 2, maxWidth: 720 }}>
      <Typography variant="body2" color="text.secondary">
        Pick a person, an action and a resource to see whether they may, and which grant decides it.
      </Typography>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start', flexWrap: 'wrap', rowGap: 1 }}>
        <TextField select size="small" label="Person" value={user} onChange={(e) => setUser(e.target.value)} sx={{ minWidth: 200 }}>
          <MenuItem value="">You</MenuItem>
          {people.map((p) => (
            <MenuItem key={p.id} value={p.id}>
              {p.name}
            </MenuItem>
          ))}
        </TextField>
        <TextField select size="small" label="Action" value={action} onChange={(e) => setAction(e.target.value)} sx={{ minWidth: 220 }}>
          {actions.map((a) => (
            <MenuItem key={a} value={a}>
              {a}
            </MenuItem>
          ))}
        </TextField>
        <ResourcePicker value={resource} onChange={setResource} tenant={tenant} app={can('grant.read', 'app')} />
        <Button
          variant="contained"
          disabled={!action || !resource}
          onClick={async () => {
            setError(undefined)
            try {
              setDecision((await Authz.explain(action, resource, user || undefined)).decision)
            } catch (err) {
              setDecision(undefined)
              setError((err as Error).message)
            }
          }}
        >
          Check
        </Button>
      </Stack>
      {decision && (
        <Alert severity={decision.allowed ? 'success' : 'warning'}>
          <strong>{decision.allowed ? 'Allowed' : 'Denied'}</strong>: {decision.reason}
        </Alert>
      )}
      {error && <Alert severity="error">{error}</Alert>}
    </Stack>
  )
}

function RolesEditor() {
  const s = useSession()
  const can = useCan()
  const [tab, setTab] = useState<'roles' | 'checker'>('roles')
  const [types, setTypes] = useState<ResourceType[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [people, setPeople] = useState<Person[]>([])
  const [selected, setSelected] = useState<string>()
  const [draft, setDraft] = useState<Draft>()
  const [pane, setPane] = useState<'permissions' | 'grants'>('permissions')
  const [error, setError] = useState<string>()
  const fail = useCallback((err: unknown) => setError((err as Error).message), [])
  const mayWrite = can('role.write')
  const tenantId = s.account?.tenantId ?? '1'

  const load = useCallback(async (select?: string) => {
    try {
      const [t, r] = await Promise.all([Authz.resourceTypes(), Authz.roles()])
      setTypes(t.types)
      setRoles(r.roles)
      const pick = r.roles.find((x) => x.id === select) ?? r.roles[0]
      if (pick) {
        setSelected(pick.id)
        setDraft(draftOf(pick))
      }
    } catch (err) {
      fail(err)
    }
  }, [fail])
  useEffect(() => void load(), [load])
  useEffect(() => {
    Promise.all([
      peopleFor('tenant', tenantId).catch(() => [] as Person[]),
      can('user.list', 'app') ? peopleFor('app', tenantId).catch(() => [] as Person[]) : Promise.resolve([] as Person[]),
    ]).then(([members, users]) => {
      const byId = new Map([...users, ...members].map((p) => [p.id, p]))
      setPeople([...byId.values()].sort((a, b) => a.name.localeCompare(b.name)))
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tenantId])

  const role = roles.find((r) => r.id === selected)
  const readOnly = !draft?.id && !!role?.builtIn
  const grouped = useMemo(
    () => [
      { title: 'Application roles', list: roles.filter((r) => r.builtIn && r.scope === 'app') },
      { title: 'Tenant roles', list: roles.filter((r) => r.builtIn && r.scope === 'tenant') },
      { title: 'Custom roles', list: roles.filter((r) => !r.builtIn) },
    ],
    [roles],
  )
  const tenantPermissions = (perms: Iterable<string>) => {
    const allowed = new Set(types.flatMap((t) => t.actions.filter((a) => a.on === 'tenant' && a.class !== 'self').map((a) => `${t.id}.${a.name}`)))
    const list = [...perms].includes('*') ? [...allowed] : [...perms].filter((p) => allowed.has(p))
    return new Set(list.filter((p) => can(p)))
  }

  const save = async () => {
    if (!draft) return
    setError(undefined)
    try {
      const resp = await Authz.saveRole({ roleId: draft.id, name: draft.name, description: draft.description, permissions: [...draft.permissions], revision: draft.revision })
      await load(resp.role?.id)
    } catch (err) {
      fail(err)
    }
  }

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Tabs value={tab} onChange={(_e, v) => setTab(v)} sx={{ borderBottom: 1, borderColor: 'divider', minHeight: 36, '& .MuiTab-root': { minHeight: 36 } }}>
        <Tab value="roles" label="Roles" />
        <Tab value="checker" label="Access checker" />
      </Tabs>
      {error && (
        <Alert severity="error" onClose={() => setError(undefined)} sx={{ m: 1 }}>
          {error}
        </Alert>
      )}
      {tab === 'checker' ? (
        <AccessChecker types={types} people={people} />
      ) : (
        <Stack direction="row" sx={{ flex: 1, minHeight: 0 }}>
          <Stack sx={{ width: 240, flexShrink: 0, borderRight: 1, borderColor: 'divider' }}>
            <Stack direction="row" sx={{ px: 1, borderBottom: 1, borderColor: 'divider' }}>
              <ToolbarAction
                title="New custom role"
                disabled={!mayWrite}
                onClick={() => {
                  setSelected(undefined)
                  setDraft({ name: 'New role', description: '', permissions: new Set(), revision: 0 })
                  setPane('permissions')
                }}
              >
                <AddIcon />
              </ToolbarAction>
              <ToolbarAction
                title="Duplicate as a custom role"
                disabled={!mayWrite || !role || role.scope === 'app'}
                onClick={() => {
                  if (!role) return
                  setSelected(undefined)
                  setDraft({ name: `${role.name} (copy)`, description: role.description ?? '', permissions: tenantPermissions(role.permissions), revision: 0 })
                  setPane('permissions')
                }}
              >
                <ContentCopyIcon />
              </ToolbarAction>
            </Stack>
            <List dense sx={{ overflow: 'auto', flex: 1 }} aria-label="Roles">
              {grouped.map((g) =>
                g.list.length === 0
                  ? null
                  : [
                      <ListSubheader key={g.title} sx={{ lineHeight: '32px' }}>
                        {g.title}
                      </ListSubheader>,
                      ...g.list.map((r) => (
                        <ListItemButton
                          key={r.id}
                          selected={r.id === selected}
                          onClick={() => {
                            setSelected(r.id)
                            setDraft(draftOf(r))
                          }}
                        >
                          <ListItemText primary={r.name} secondary={r.description} slotProps={{ secondary: { noWrap: true } }} />
                        </ListItemButton>
                      )),
                    ],
              )}
            </List>
          </Stack>
          <Box sx={{ flex: 1, minWidth: 0, overflow: 'auto', p: 1.5 }}>
            {draft && (
              <Stack spacing={1.5}>
                <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                  {readOnly ? (
                    <Typography variant="h6" component="h2">
                      {draft.name}
                    </Typography>
                  ) : (
                    <TextField size="small" label="Name" value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} disabled={!mayWrite} />
                  )}
                  {role?.builtIn && <Chip size="small" label="Built-in" />}
                  {(role?.scope ?? 'tenant') === 'app' ? <Chip size="small" variant="outlined" label="Granted on the application" /> : <Chip size="small" variant="outlined" label="Granted in a tenant" />}
                  <Box sx={{ flex: 1 }} />
                  {!readOnly && mayWrite && (
                    <>
                      <Button startIcon={<SaveIcon />} variant="contained" onClick={() => void save()}>
                        Save
                      </Button>
                      {draft.id && (
                        <Button
                          color="error"
                          startIcon={<DeleteIcon />}
                          onClick={async () => {
                            try {
                              await Authz.deleteRole(draft.id!)
                              await load()
                            } catch (err) {
                              fail(err)
                            }
                          }}
                        >
                          Delete
                        </Button>
                      )}
                    </>
                  )}
                </Stack>
                {readOnly ? (
                  <Stack spacing={0.5}>
                    <Typography variant="body2">{draft.description}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      Built-in roles cannot be changed; duplicate a tenant role to make a custom one.
                    </Typography>
                  </Stack>
                ) : (
                  <TextField size="small" label="Description" value={draft.description} onChange={(e) => setDraft({ ...draft, description: e.target.value })} disabled={!mayWrite} />
                )}
                {role && (
                  <Tabs value={pane} onChange={(_e, v) => setPane(v)} sx={{ minHeight: 32, '& .MuiTab-root': { minHeight: 32 } }}>
                    <Tab value="permissions" label="Permissions" />
                    <Tab value="grants" label="Grants" disabled={!can('grant.read', role.scope === 'app' ? 'app' : undefined)} />
                  </Tabs>
                )}
                {pane === 'grants' && role ? (
                  <Grants key={role.id} role={role} people={people} fail={fail} />
                ) : (
                  <Matrix types={types} draft={draft} scope={role?.scope ?? 'tenant'} readOnly={readOnly || !mayWrite} onChange={(p) => setDraft({ ...draft, permissions: p })} />
                )}
              </Stack>
            )}
          </Box>
        </Stack>
      )}
    </Stack>
  )
}

export default RolesEditor
