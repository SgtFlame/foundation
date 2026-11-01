// The editor of every app editor kind (documentation/ServerUI.md › App
// editor kinds): it opens the kind's page as a UI session, renders its
// tree, shows who else has the same object open, reports the page's
// subject to the workbench and performs the client actions handlers return.

import { Alert, Box, Chip, CircularProgress, Snackbar, Stack, Typography } from '@mui/material'
import { Suspense, useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import { live } from '../live/LiveConnection.ts'
import { PresenceAvatars } from '../live/PresenceAvatars.tsx'
import { usePresence } from '../live/usePresence.ts'
import { log } from '../logging/index.ts'
import type { EditorProps } from '../workbench/points.ts'
import { useWorkbenchSubject } from '../workbench/context/WorkbenchContext.ts'
import { AppEditorObject, parseKind } from './AppEditorObject.ts'
import type { UiAction } from './types.ts'
import { UiHostContext, type UiHost } from './UiHostContext.ts'
import UiRenderer from './UiRenderer.tsx'
import { UiSession, type UiSessionState } from './UiSession.ts'

const logger = log.In('ui').Tags('host')

// Open sessions by id: a remount (StrictMode, a moved tab) keeps the
// session; it closes once nothing holds it, a moment after its last unmount
const open = new Map<string, { session: UiSession; holders: number; closing?: ReturnType<typeof setTimeout> }>()

function acquire(id: string, page: string, params: unknown): UiSession {
  let entry = open.get(id)
  if (!entry || entry.session.page !== page) {
    const session = new UiSession(id, page, params)
    entry = { session, holders: 0 }
    open.set(id, entry)
  }
  return entry.session
}

function hold(session: UiSession) {
  const entry = open.get(session.id)
  if (!entry || entry.session !== session) return
  clearTimeout(entry.closing)
  entry.holders++
}

function release(session: UiSession, forget: () => boolean) {
  const entry = open.get(session.id)
  if (!entry || entry.session !== session) return
  entry.holders--
  if (entry.holders > 0) return
  entry.closing = setTimeout(() => {
    open.delete(session.id)
    session.close(forget())
  }, 0)
}

export interface AppHostProps extends EditorProps<AppEditorObject> {
  page: string // "<appId>/<page>"
}

function AppHost({ object, registry, tabId, page }: AppHostProps) {
  const [toast, setToast] = useState<{ message: string; severity: 'success' | 'info' | 'warning' | 'error' }>()

  const perform = useCallback(
    (s: UiSession, actions: UiAction[]) => {
      for (const a of actions) {
        switch (a.type) {
          case 'toast':
            setToast({ message: a.message ?? '', severity: a.severity ?? 'info' })
            break
          case 'openEditor':
            if (a.kind && a.id) registry.edit(new AppEditorObject({ kind: a.kind, id: a.id, name: a.name, helpText: a.helpText, params: a.params }))
            break
          case 'rename':
            if (a.name) registry.rename(tabId, a.name)
            break
          case 'copy':
            void navigator.clipboard.writeText(a.text ?? '')
            break
          case 'download': {
            const link = document.createElement('a')
            link.href = a.url ?? ''
            link.download = a.fileName ?? ''
            link.click()
            break
          }
          case 'confirm':
            if (a.handler) s.emit([], { handler: a.handler, payload: { confirmed: window.confirm(a.message ?? 'Are you sure?') } })
            break
          default:
            logger.With('action', a.type).warn('the %s action is not supported yet', a.type)
        }
      }
    },
    [registry, tabId],
  )

  // One session per tab, resumed by its id after a reload
  const id = (object.session || tabId).replace(/[^A-Za-z0-9_-]/g, '_')
  const session = acquire(id, page, object.params)
  useEffect(() => {
    session.setActionHandler((actions) => perform(session, actions))
    return () => session.setActionHandler(undefined)
  }, [session, perform])
  useEffect(() => {
    hold(session)
    // The tab is gone, rather than moved, when the layout no longer has it
    return () => release(session, () => !registry.isOpen(tabId))
  }, [session, registry, tabId])

  const state: UiSessionState = useSyncExternalStore(session.subscribe, session.getState)
  const status = useSyncExternalStore((l) => live.onStatus(l), () => live.getStatus())

  // Presence in the tab's object, with what this window has selected
  const { app, kind } = parseKind(object.kind)
  const [presenceState, setPresenceState] = useState<Record<string, unknown>>({})
  const presence = usePresence(`app_${app}_${kind}.${object.id.replace(/[^A-Za-z0-9_-]/g, '_')}`, presenceState)
  const host: UiHost = useMemo(
    () => ({ others: presence.others, setPresence: (s) => setPresenceState((old) => (JSON.stringify({ ...old, ...s }) === JSON.stringify(old) ? old : { ...old, ...s })) }),
    [presence.others],
  )
  useWorkbenchSubject(tabId, state.subject)

  return (
    <UiHostContext.Provider value={host}>
      <Stack sx={{ height: '100%' }}>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', px: 1, py: 0.5, borderBottom: 1, borderColor: 'divider', minHeight: 36 }}>
          <Typography variant="subtitle2" sx={{ flex: 1 }} noWrap>
            {state.title ?? object.name}
          </Typography>
          {status !== 'open' && <Chip size="small" color="warning" label={status === 'connecting' ? 'Reconnecting…' : 'Offline'} />}
          <PresenceAvatars users={presence.others} />
        </Stack>
        {state.error && (
          <Alert severity="error" onClose={() => session.clearError()} sx={{ m: 1 }}>
            {state.error.message}
          </Alert>
        )}
        <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
          {state.tree ? (
            <Suspense fallback={<CircularProgress size={24} sx={{ m: 2 }} />}>
              <UiRenderer session={session} tree={state.tree} />
            </Suspense>
          ) : state.status === 'opening' ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
              <CircularProgress size={24} />
            </Box>
          ) : null}
        </Box>
      </Stack>
      <Snackbar open={!!toast} autoHideDuration={4000} onClose={() => setToast(undefined)}>
        <Alert severity={toast?.severity ?? 'info'} onClose={() => setToast(undefined)} variant="filled">
          {toast?.message}
        </Alert>
      </Snackbar>
    </UiHostContext.Provider>
  )
}

export default AppHost
