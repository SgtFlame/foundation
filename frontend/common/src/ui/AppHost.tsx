// The editor of every app editor kind (documentation/ServerUI.md › App
// editor kinds; documentation/Apps.md › Built in slice 34): it opens the
// kind's page as a UI session, renders its tree, shows who else has the
// same object open, reports the page's subject to the workbench, follows
// the page's title in the tab's name and performs the client actions
// handlers return. An app's main tab is drawn in its frame, which shows
// the title and presence, so it has no header of its own.

import { Alert, Box, Chip, CircularProgress, Snackbar, Stack, Typography } from '@mui/material'
import { Suspense, useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import { live } from '../live/LiveConnection.ts'
import { PresenceAvatars } from '../live/PresenceAvatars.tsx'
import { usePresence, type PresentUser } from '../live/usePresence.ts'
import { useQuery } from '../live/useQuery.ts'
import { log } from '../logging/index.ts'
import type { EditorProps } from '../workbench/points.ts'
import { useWorkbenchSubject } from '../workbench/context/WorkbenchContext.ts'
import { AppEditorObject, parseKind } from './AppEditorObject.ts'
import { onNavigateRequest, openAppAt } from './editorKinds.ts'
import type { UiAction, UiNode } from './types.ts'
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

// Whether a presence.avatars node in the tree joins object: the page is
// present there itself, and the tab must not join it a second time
function joins(node: UiNode | undefined, object: string): boolean {
  if (!node) return false
  if (node.type === 'presence.avatars' && node.props?.join === object) return true
  return (node.children ?? []).some((c) => joins(c, object)) || Object.values(node.slots ?? {}).some((list) => list.some((c) => joins(c, object)))
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
          case 'navigate':
            // From a page outside the app's main tab: open the app there
            if (a.page) openAppAt(registry, parseKind(object.kind).app || page.split('/')[0], a.page)
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
    [registry, tabId, object.kind, page],
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

  // An app's main tab goes to the routes deep links and other tabs ask for.
  // A tab of another kind (Forge's draft run) names its app by its page
  const parsed = parseKind(object.kind)
  const app = parsed.app || page.split('/')[0]
  const kind = parsed.app ? parsed.kind : page.split('/')[1]
  useEffect(() => {
    if (kind !== 'main') return
    return onNavigateRequest(tabId, (route) => session.emit([], { handler: 'app.navigate', payload: { route } }))
  }, [kind, tabId, session])
  // The tab is named by its page's title (an app's main tab, its name)
  useEffect(() => {
    if (state.title && state.title !== object.name) registry.rename(tabId, state.title)
  }, [state.title, object.name, registry, tabId])
  // An app uninstalled, or a page an upgrade removed: the tab closes
  useEffect(() => {
    if (state.error?.errorCode === 'app_page_gone') registry.close(tabId)
  }, [state.error, registry, tabId])

  // Presence in the page's subject (its record), else the tab's object,
  // with what this window has selected; when the page joins it itself (a
  // presence bar), the tab only shows who is there
  const subjectObject = state.subject?.object ?? `app_${app}_${kind}.${object.id.replace(/[^A-Za-z0-9_-]/g, '_')}`
  const joined = useMemo(() => joins(state.tree, subjectObject), [state.tree, subjectObject])
  const [presenceState, setPresenceState] = useState<Record<string, unknown>>({})
  const presence = usePresence(joined ? undefined : subjectObject, presenceState)
  const { data: present } = useQuery<PresentUser[]>(joined ? 'live.presence' : undefined, { object: subjectObject })
  const others = useMemo(() => (joined ? (present ?? []).filter((u) => u.connection !== presence.me) : presence.others), [joined, present, presence.me, presence.others])
  const host: UiHost = useMemo(
    () => ({ others, setPresence: (s) => setPresenceState((old) => (JSON.stringify({ ...old, ...s }) === JSON.stringify(old) ? old : { ...old, ...s })) }),
    [others],
  )
  useWorkbenchSubject(tabId, state.subject)

  return (
    <UiHostContext.Provider value={host}>
      <Stack sx={{ height: '100%' }}>
        {state.framed ? (
          status !== 'open' && <Chip size="small" color="warning" label={status === 'connecting' ? 'Reconnecting…' : 'Offline'} sx={{ alignSelf: 'flex-end', m: 0.5 }} />
        ) : (
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center', px: 1, py: 0.5, borderBottom: 1, borderColor: 'divider', minHeight: 36 }}>
            <Typography variant="subtitle2" sx={{ flex: 1 }} noWrap>
              {state.title ?? object.name}
            </Typography>
            {status !== 'open' && <Chip size="small" color="warning" label={status === 'connecting' ? 'Reconnecting…' : 'Offline'} />}
            <PresenceAvatars users={others} />
          </Stack>
        )}
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
