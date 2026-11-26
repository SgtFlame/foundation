// One section's form: the contributor's Editor or the generic form, with
// dirty tracking, Save and Discard, server errors shown at their paths, and
// a conflict when the section was saved elsewhere meanwhile.

import { useState } from 'react'
import { Alert, Box, Button, Chip, CircularProgress, Paper, Stack, Typography } from '@mui/material'
import { log } from '../../logging/index.ts'
import { Config, ServiceError, type FieldError, type Scope, type View } from '../../services/Config.ts'
import { configStore, useConfig } from '../../settings/configStore.ts'
import type { ConfigurationContribution } from '../../settings/points.ts'
import { getAt, leaves, patchFrom, setAt, type Schema, type Value } from '../../settings/schema.ts'
import { SchemaField, type FieldContext } from './SchemaField.tsx'

const settingsLog = log.In('settings')

// readOnly shows the section at the scope without letting its plain
// settings change: the user lacks config.write there (RBAC question 1).
// secretsReadOnly does the same for its secrets, which have their own
// permission (Secrets question 7), so an Application Administrator can set
// the app's keys and nothing else.
// hasSecrets tells whether a schema has a secret field
function hasSecrets(schema: Schema): boolean {
  if (schema.type === 'secret') return true
  return Object.values(schema.properties ?? {}).some(hasSecrets) || (!!schema.items && hasSecrets(schema.items))
}

function SectionPanel({ section, scope, readOnly = false, secretsReadOnly = readOnly }: { section: ConfigurationContribution; scope: Scope; readOnly?: boolean; secretsReadOnly?: boolean }) {
  const schema = section.schema as Schema
  const locked = readOnly && (secretsReadOnly || !hasSecrets(schema))
  const entry = useConfig(section.id, scope)
  // The view editing started from: its value is what the patch is computed
  // against and its revision what the server checks
  const [base, setBase] = useState<View>()
  const [draft, setDraft] = useState<Value>()
  const [resets, setResets] = useState<ReadonlySet<string>>(new Set())
  const [errors, setErrors] = useState<FieldError[]>([])
  const [conflict, setConflict] = useState(false)
  const [failure, setFailure] = useState<string>()
  const [saving, setSaving] = useState(false)

  if (!section.server) {
    return (
      <Panel section={section}>
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>
          This section is declared in the browser only, and browser sections are not stored yet.
        </Typography>
      </Panel>
    )
  }
  if (!entry.view) {
    return <Panel section={section}>{entry.error ? <Alert severity="error">{entry.error}</Alert> : <CircularProgress size={20} />}</Panel>
  }

  const start = base ?? entry.view
  const original = start.value ?? {}
  const value = draft ?? original
  const patch = patchFrom(schema, original, value, resets)
  const dirty = Object.keys(patch).length > 0
  const changedElsewhere = base !== undefined && entry.view.revision !== base.revision

  const edit = (next: Value) => {
    if (!base) setBase(entry.view)
    setDraft(next)
  }
  const discard = () => {
    setBase(undefined)
    setDraft(undefined)
    setResets(new Set())
    setErrors([])
    setConflict(false)
    setFailure(undefined)
  }
  const save = async (revision = start.revision) => {
    setSaving(true)
    setFailure(undefined)
    try {
      const secret = leaves(schema).some((l) => l.schema.type === 'secret' && typeof getAt(patch, l.path) === 'string')
      const resp = await Config.set({ section: section.id, scope, patch, revision }, { secret })
      if (resp.errors?.length) {
        setErrors(resp.errors)
        return
      }
      configStore.put(resp)
      discard()
      settingsLog.With('section', section.id).info('saved %s', section.title)
    } catch (err) {
      if (err instanceof ServiceError && err.status === 409) {
        setConflict(true)
      } else {
        settingsLog.fromError(err).With('section', section.id).warn('cannot save %s', section.title)
        setFailure(err instanceof Error ? err.message : String(err))
      }
    } finally {
      setSaving(false)
    }
  }
  // Saves this form's changes over whatever was saved elsewhere
  const overwrite = async () => {
    setConflict(false)
    try {
      const fresh = await Config.get({ section: section.id, scope })
      configStore.put(fresh)
      await save(fresh.revision)
    } catch (err) {
      setFailure(err instanceof Error ? err.message : String(err))
    }
  }

  const ctx: FieldContext = {
    sectionId: section.id,
    rootSchema: schema,
    sectionScopes: section.scopes,
    scope,
    readOnly,
    secretsReadOnly,
    original,
    draft: value,
    sources: start.sources ?? {},
    resets,
    toggleReset: (path) => {
      if (!base) setBase(entry.view)
      setResets((old) => {
        const next = new Set(old)
        if (!next.delete(path)) next.add(path)
        return next
      })
    },
    errors: Object.fromEntries(errors.map((e) => [e.path, e.message])),
    onChange: (path, v) => edit(setAt(value, path, v)),
  }
  const Editor = section.Editor

  return (
    <Panel
      section={section}
      actions={
        locked ? (
          <Chip size="small" label="Read-only" title={`You may not change ${section.title} at the ${scope} scope`} />
        ) : (
          <Stack direction="row" spacing={1}>
            <Button size="small" disabled={!dirty || saving} onClick={discard}>
              Discard
            </Button>
            <Button size="small" variant="contained" disabled={!dirty || saving} onClick={() => void save()}>
              Save
            </Button>
          </Stack>
        )
      }
    >
      {(conflict || changedElsewhere) && (
        <Alert
          severity="warning"
          sx={{ mb: 1.5 }}
          action={
            <Stack direction="row">
              <Button size="small" color="inherit" onClick={discard}>
                Reload
              </Button>
              {dirty && (
                <Button size="small" color="inherit" onClick={() => void overwrite()}>
                  Overwrite
                </Button>
              )}
            </Stack>
          }
        >
          Changed elsewhere since you started editing.
        </Alert>
      )}
      {failure && (
        <Alert severity="error" sx={{ mb: 1.5 }}>
          {failure}
        </Alert>
      )}
      {errors.length > 0 && (
        <Alert severity="error" sx={{ mb: 1.5 }}>
          {errors.map((e) => (
            <div key={e.path}>
              <code>{e.path || section.id}</code> {e.message}
            </div>
          ))}
        </Alert>
      )}
      {Editor ? (
        <Editor value={value} onChange={(v) => edit(v as Value)} schema={schema} errors={ctx.errors} />
      ) : (
        <SchemaField schema={schema} path="" value={value} ctx={ctx} />
      )}
    </Panel>
  )
}

function Panel({ section, actions, children }: { section: ConfigurationContribution; actions?: React.ReactNode; children: React.ReactNode }) {
  return (
    <Paper variant="outlined" sx={{ p: 1.5, mb: 2 }}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 0.5 }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600, whiteSpace: 'nowrap' }}>
          {section.title}
        </Typography>
        <Chip
          size="small"
          variant="outlined"
          label={section.id}
          sx={{
            height: 18,
            fontSize: 11,
            fontFamily: 'monospace',
            minWidth: 0,
          }}
        />
        <Box sx={{ flex: 1 }} />
        {actions}
      </Stack>
      {section.description && (
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
          {section.description}
        </Typography>
      )}
      {children}
    </Paper>
  )
}

export default SectionPanel
