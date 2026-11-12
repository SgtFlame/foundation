// The apps' editor kinds as workbench editors (documentation/ServerUI.md ›
// App editor kinds; documentation/Apps.md › Built in slice 34): the live
// ui.editorKinds query (Go apps and the tenant's installed ones), then a
// workbench.editor app:<appId>:<kind> showing AppHost on the kind's page,
// and an Apps command per app with a main tab, shown to those who may use
// the app (its reach, through canIn). Contributions are replaced whenever
// an app is installed, upgraded or uninstalled.

import { createElement, lazy, useSyncExternalStore, type ComponentType } from 'react'
import { live } from '../live/LiveConnection.ts'
import { applyDelta } from '../live/useQuery.ts'
import { log } from '../logging/index.ts'
import { canIn } from '../session/permissions.ts'
import { Ui, type AppInfo, type EditorKindInfo } from '../services/Ui.ts'
import { extensions } from '../workbench/extensions/index.ts'
import { commands, editors, type EditorProps } from '../workbench/points.ts'
import { AppEditorObject } from './AppEditorObject.ts'
import { iconComponent } from './renderers/mui/iconMap.ts'

const logger = log.In('ui').Tags('kinds')

const AppHost = lazy(() => import('./AppHost.tsx'))

export interface InstalledApps {
  apps: AppInfo[]
  kinds: EditorKindInfo[]
  loaded: boolean
}

let installed: InstalledApps = { apps: [], kinds: [], loaded: false }
const listeners = new Set<() => void>()
let removers: (() => void)[] = []
// Kinds an uninstall or upgrade removed: their open tabs close
const removedListeners = new Set<(kinds: string[]) => void>()

// The workbench closes the tabs of removed kinds
export function onKindsRemoved(listener: (kinds: string[]) => void): () => void {
  removedListeners.add(listener)
  return () => {
    removedListeners.delete(listener)
  }
}

interface Registry {
  edit(o: AppEditorObject): void
  isOpen(tabId: string): boolean
}

// An open main tab asked to go to a route (a deep link, a navigate action);
// a route asked for before its tab has mounted waits for it
const navigators = new Map<string, (route: string) => void>()
const pending = new Map<string, string>()

// AppHost registers its main tab's navigation here while it is open
export function onNavigateRequest(tabId: string, go: (route: string) => void): () => void {
  navigators.set(tabId, go)
  const route = pending.get(tabId)
  pending.delete(tabId)
  if (route) go(route)
  return () => {
    if (navigators.get(tabId) === go) navigators.delete(tabId)
  }
}

// Opens an app's own tab, at route when given: an open tab goes there, a
// new one opens there
export function openApp(registry: Registry, app: Pick<AppInfo, 'id' | 'title' | 'description'>, route?: string) {
  const object = new AppEditorObject({ kind: `app:${app.id}:main`, id: 'main', name: app.title, helpText: app.description, params: route ? { route } : {} })
  const tabId = `${object.kind}:${object.id}`
  const open = registry.isOpen(tabId)
  registry.edit(object)
  if (!open || !route) return
  const go = navigators.get(tabId)
  if (go) go(route)
  else pending.set(tabId, route)
}

// A deep link, /workbench?app=<appId>&route=/task/123, opens the app there
export function openDeepLink(registry: Registry, search: string) {
  const params = new URLSearchParams(search)
  const app = params.get('app')
  if (app) openAppAt(registry, app, params.get('route') ?? '')
}

// Opens an app's own tab at a route, by the app's id
export function openAppAt(registry: Registry, appId: string, route: string) {
  const app = installed.apps.find((a) => a.id === appId) ?? { id: appId, title: appId, description: '' }
  openApp(registry, app, route)
}

// Who may use an app: anyone who may read the UI, or any one of its reach
function mayUse(app: AppInfo, ctx: Record<string, unknown>): boolean {
  if (!app.reach?.length) return true
  return app.reach.some((r) => canIn(ctx, r.action, r.resource ? `tenant:${ctx.tenantId}/${r.resource}` : undefined))
}

function apply(apps: AppInfo[], kinds: EditorKindInfo[]) {
  for (const remove of removers) remove()
  removers = []
  for (const k of kinds) {
    const page = k.page
    const component: ComponentType<EditorProps<AppEditorObject>> = (props) => createElement(AppHost, { ...props, page })
    removers.push(extensions.contribute(editors, { id: k.id, source: `app:${k.app}`, name: k.title, restorable: true, component }))
  }
  for (const [i, app] of apps.entries()) {
    if (!kinds.some((k) => k.app === app.id && k.kind === 'main')) continue
    removers.push(
      extensions.contribute(commands, {
        id: `open-app-${app.id}`,
        source: `app:${app.id}`,
        order: 500 + i,
        title: app.title,
        icon: iconComponent(app.icon),
        section: 'Apps',
        when: (ctx) => mayUse(app, ctx),
        run: ({ registry }) => openApp(registry, app),
      }),
    )
  }
  const removed = installed.loaded ? installed.kinds.filter((k) => !kinds.some((n) => n.id === k.id)).map((k) => k.id) : []
  installed = { apps, kinds, loaded: true }
  for (const l of listeners) l()
  if (removed.length) for (const l of removedListeners) l(removed)
}

type Kinds = { apps: AppInfo[]; kinds: EditorKindInfo[] }

export function loadEditorKinds(): () => void {
  let current: Kinds | undefined
  const set = (next: Kinds) => {
    current = next
    apply(next.apps ?? [], next.kinds ?? [])
  }
  return live.subscribe(
    'ui.editorKinds',
    {},
    {
      onSnapshot: (data) => set(data as Kinds),
      onChange: (delta) => set(applyDelta(current, delta)),
      onError: (err) => {
        logger.fromError(err).warn('cannot follow the applications; loading them once')
        Ui.editorKinds().then(
          (resp) => set(resp),
          (e) => logger.fromError(e).warn('cannot load the applications'),
        )
      },
    },
  )
}

// The installed apps, re-rendering when they change
export function useInstalledApps(): InstalledApps {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => installed,
  )
}
