// The installed apps' editor kinds as workbench editors
// (documentation/ServerUI.md › App editor kinds): GET /ui/editor-kinds, then
// a workbench.editor app:<appId>:<kind> showing AppHost on the kind's page,
// and an Apps command per app opening its main tab. Contributions
// are replaced when loaded again.

import { createElement, lazy, useSyncExternalStore, type ComponentType } from 'react'
import { log } from '../logging/index.ts'
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

// Opens an app's own tab
export function openApp(registry: { edit(o: AppEditorObject): void }, app: AppInfo) {
  registry.edit(new AppEditorObject({ kind: `app:${app.id}:main`, id: 'main', name: app.title, helpText: app.description, params: {} }))
}

export async function loadEditorKinds(): Promise<void> {
  let resp
  try {
    resp = await Ui.editorKinds()
  } catch (err) {
    logger.fromError(err).warn('cannot load the applications')
    return
  }
  for (const remove of removers) remove()
  removers = []
  for (const k of resp.kinds) {
    const page = k.page
    const component: ComponentType<EditorProps<AppEditorObject>> = (props) => createElement(AppHost, { ...props, page })
    removers.push(
      extensions.contribute(editors, { id: k.id, source: `app:${k.app}`, name: k.title, restorable: true, component }),
    )
  }
  for (const [i, app] of resp.apps.entries()) {
    if (!resp.kinds.some((k) => k.app === app.id && k.kind === 'main')) continue
    removers.push(
      extensions.contribute(commands, {
        id: `open-app-${app.id}`,
        source: `app:${app.id}`,
        order: 500 + i,
        title: app.title,
        icon: iconComponent(app.icon),
        section: 'Apps',
        run: ({ registry }) => openApp(registry, app),
      }),
    )
  }
  installed = { apps: resp.apps, kinds: resp.kinds, loaded: true }
  for (const l of listeners) l()
}

// The installed apps, re-rendering when they load
export function useInstalledApps(): InstalledApps {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => installed,
  )
}
