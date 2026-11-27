// connection.form (forge 07c): makes or edits a connection. It picks a
// connector type implementing the contract (an offline one shown as
// such), draws its settings from the type's settings schema with
// Configuration's SchemaField, shows its secrets as write-only fields
// (a stored secret is only "set"; an empty field keeps it), and has Test
// (the connection.test call) and Save (connection.create or
// connection.update, sent with {secret: true} so DevTools records no
// secret). Generic over every connector type, scripted and announced ones
// included. After a create it goes on editing the new connection. A type
// with OAuth (forge 26a-1) shows who signed it in and Sign in with …
// (Sign in again once its sign-in expired), which opens the provider in a
// popup (the connection.signIn call) and hears the callback's result; with
// popups blocked, the page itself goes and comes back.

import { useEffect, useMemo, useState } from 'react'
import { Alert, Box, Button, Chip, FormControl, InputLabel, MenuItem, Select, Stack, TextField, Typography } from '@mui/material'
import { live } from '../../../live/LiveConnection.ts'
import { useQuery } from '../../../live/useQuery.ts'
import type { ConnectionDescription, ConnectionTestResult, OAuthResult, TypeList } from '../../../services/Connectors.ts'
import type { Schema } from '../../../settings/schema.ts'
import { SchemaField, type FieldContext } from '../../../tabs/settings/SchemaField.tsx'
import { getAt, setAt } from '../../../settings/schema.ts'
import type { RendererProps } from '../../points.ts'

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

// splitSchema is the settings schema without its secret fields, and those
function splitSchema(raw: unknown): { plain: Schema; secrets: [string, Schema][] } {
  const s = (raw ?? { type: 'object', properties: {} }) as Schema
  const plain: Schema = { ...s, properties: {} }
  const secrets: [string, Schema][] = []
  for (const [k, p] of Object.entries(s.properties ?? {})) {
    if (p.type === 'secret') secrets.push([k, p])
    else plain.properties![k] = p
  }
  return { plain, secrets }
}

export function ConnectionForm(p: RendererProps) {
  // A connection this form made goes on being edited here (Test, then
  // Save again) until the connection prop names another
  const [made, setMade] = useState('')
  const id = str(p.props.connection) || made
  const contract = str(p.props.contract)
  const owner = str(p.props.owner) || 'me'
  const disabled = !!p.props.disabled
  const types = useQuery<TypeList>('connectors.types', contract ? { contract, direction: 'outbound' } : { direction: 'outbound' })
  const described = useQuery<ConnectionDescription>(id ? 'connections.describe' : undefined, { id })
  const existing = described.data?.connection ?? undefined

  const [type, setType] = useState('')
  const [name, setName] = useState<string | undefined>()
  const [settings, setSettings] = useState<Record<string, unknown> | undefined>()
  const [secrets, setSecrets] = useState<Record<string, string>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [message, setMessage] = useState<{ severity: 'success' | 'error' | 'info'; text: string }>()
  const [busy, setBusy] = useState(false)

  const choices = (types.data?.types ?? []).filter((t) => !t.type.noConnection)
  const typeId = existing?.type ?? (type || (choices.length === 1 ? choices[0].type.id : ''))
  const info = choices.find((t) => t.type.id === typeId) ?? described.data?.type
  const { plain, secrets: secretFields } = useMemo(() => splitSchema(info?.type.settings), [info?.type.settings])
  const shownName = name ?? existing?.name ?? ''
  const shownSettings = settings ?? existing?.settings ?? {}

  const ctx: FieldContext = {
    sectionId: 'connection',
    rootSchema: plain,
    sectionScopes: ['user'],
    scope: 'user',
    readOnly: disabled,
    original: existing?.settings ?? {},
    draft: shownSettings,
    sources: {},
    resets: new Set(),
    toggleReset: () => undefined,
    errors,
    onChange: (path, value) => setSettings(setAt(shownSettings, path, value) as Record<string, unknown>),
  }

  // The settings a save sends: all of them on create, only what changed
  // on update (a merge; null removes)
  const changed = (): Record<string, unknown> => {
    if (!existing) return shownSettings
    const out: Record<string, unknown> = {}
    const keys = new Set([...Object.keys(existing.settings ?? {}), ...Object.keys(shownSettings)])
    for (const k of keys) {
      const a = JSON.stringify(getAt(existing.settings, k) ?? null)
      const b = JSON.stringify(getAt(shownSettings, k) ?? null)
      if (a !== b) out[k] = getAt(shownSettings, k) ?? null
    }
    return out
  }

  const fail = (err: unknown) => {
    const e = err as { message?: string; problems?: { path?: string; message: string }[] }
    const byPath: Record<string, string> = {}
    for (const pr of e.problems ?? []) {
      if (pr.path?.startsWith('settings.')) byPath[pr.path.slice('settings.'.length)] = pr.message
      if (pr.path?.startsWith('secrets.')) byPath['$' + pr.path.slice('secrets.'.length)] = pr.message
    }
    setErrors(byPath)
    setMessage({ severity: 'error', text: e.problems?.[0]?.message ?? e.message ?? 'Not saved' })
  }

  const save = async () => {
    setBusy(true)
    setMessage(undefined)
    setErrors({})
    try {
      const filled = Object.fromEntries(Object.entries(secrets).filter(([, v]) => v !== ''))
      if (existing) {
        if (shownName !== existing.name) await live.command('connection.rename', existing.id, { name: shownName })
        await live.command('connection.update', existing.id, { settings: changed(), secrets: filled }, { secret: true })
        p.emit('saved', { id: existing.id })
      } else {
        const ack = await live.command('connection.create', '', { type: typeId, name: shownName, owner, settings: shownSettings, secrets: filled }, { secret: true })
        if (ack.target) setMade(ack.target)
        p.emit('saved', { id: ack.target })
      }
      setSecrets({})
      setSettings(undefined)
      setName(undefined)
      setMessage({ severity: 'success', text: 'Saved' })
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  // The OAuth popup's answer
  useEffect(() => {
    const heard = (e: MessageEvent) => {
      const r = e.data as OAuthResult
      if (e.origin !== window.location.origin || r?.type !== 'connector.oauth' || r.connection !== existing?.id) return
      setBusy(false)
      setMessage(r.error ? { severity: 'error', text: r.message || r.error } : { severity: 'success', text: r.account ? `Signed in as ${r.account}` : 'Signed in' })
      p.emit('signedIn', { ok: !r.error, account: r.account, error: r.error })
    }
    window.addEventListener('message', heard)
    return () => window.removeEventListener('message', heard)
  }, [existing?.id, p])

  const signIn = async () => {
    if (!existing) return
    // Opened in the click, so no popup blocker takes it for an ad
    const popup = window.open('', 'connector-oauth', 'popup,width=520,height=680')
    setBusy(true)
    setMessage({ severity: 'info', text: 'Signing in…' })
    try {
      const back = window.location.pathname + window.location.search
      let url = ''
      await live.call('connection.signIn', { id: existing.id, return: back }, (event, data) => {
        if (event === 'result') url = (data as { url: string }).url
      })
      if (popup) popup.location.href = url
      else window.location.assign(url)
    } catch (err) {
      popup?.close()
      setBusy(false)
      setMessage({ severity: 'error', text: (err as Error).message })
    }
  }

  const test = async () => {
    if (!existing) return
    setBusy(true)
    setMessage({ severity: 'info', text: 'Testing…' })
    try {
      await live.call('connection.test', { id: existing.id }, (event, data) => {
        if (event !== 'result') return
        const r = data as ConnectionTestResult
        setMessage(r.ok ? { severity: 'success', text: 'The connection works' } : { severity: 'error', text: `${r.error?.code}: ${r.error?.message}` })
        p.emit('tested', { ok: r.ok, error: r.error })
      })
    } catch (err) {
      setMessage({ severity: 'error', text: (err as Error).message })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Stack spacing={2} sx={{ maxWidth: 560 }}>
      {existing ? (
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <Typography variant="body2" color="text.secondary">
            {info?.type.meta.title ?? existing.type}
          </Typography>
          <Chip size="small" label={existing.status} color={existing.status === 'ok' ? 'success' : existing.status === 'error' ? 'error' : 'default'} />
          {existing.lastError && (
            <Typography variant="caption" color="error">
              {existing.lastError}
            </Typography>
          )}
        </Stack>
      ) : (
        <FormControl size="small" fullWidth disabled={disabled}>
          <InputLabel id="connection-type">Connector type</InputLabel>
          <Select labelId="connection-type" label="Connector type" value={typeId} onChange={(e) => setType(e.target.value)}>
            {choices.map((t) => (
              <MenuItem key={t.type.id} value={t.type.id}>
                {t.type.meta.title}
                {!t.online && <Chip size="small" label="offline" sx={{ ml: 1 }} />}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      )}
      {existing && described.data?.oauth && (
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <Button variant={existing.account && existing.status !== 'error' ? 'outlined' : 'contained'} disabled={disabled || busy} onClick={() => void signIn()}>
            {existing.account ? 'Sign in again' : `Sign in with ${described.data.oauth.title}`}
          </Button>
          <Typography variant="body2" color="text.secondary">
            {existing.account ? `Signed in as ${existing.account}` : 'Not signed in yet'}
          </Typography>
        </Stack>
      )}
      {!existing && info?.type.oauth && <Alert severity="info">Save it, then sign in.</Alert>}
      {info && !info.online && <Alert severity="warning">No adapter serves {info.type.meta.title} right now: it can be saved, not tested or used.</Alert>}
      {typeId && (
        <>
          <TextField size="small" label="Name" required value={shownName} disabled={disabled} onChange={(e) => setName(e.target.value)} />
          <SchemaField schema={plain} path="" value={shownSettings} ctx={ctx} />
          {secretFields.map(([k, s]) => {
            const set = existing?.secrets?.[k]?.set
            return (
              <TextField key={k} size="small" type="password" autoComplete="new-password" label={s.title ?? k} disabled={disabled}
                value={secrets[k] ?? ''} onChange={(e) => setSecrets({ ...secrets, [k]: e.target.value })}
                error={!!errors['$' + k]} helperText={errors['$' + k] ?? (set ? 'Set: leave empty to keep it' : s.description)} />
            )
          })}
          <Box>
            <Stack direction="row" spacing={1}>
              <Button variant="contained" disabled={disabled || busy || !shownName.trim()} onClick={() => void save()}>
                {str(p.props.saveLabel) || 'Save'}
              </Button>
              <Button disabled={disabled || busy || !existing} onClick={() => void test()}>
                Test
              </Button>
            </Stack>
          </Box>
        </>
      )}
      {message && <Alert severity={message.severity}>{message.text}</Alert>}
    </Stack>
  )
}
