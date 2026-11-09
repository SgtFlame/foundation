import { Layout, Model, TabNode, TabSetNode, type IJsonModel } from 'flexlayout-react'
import 'flexlayout-react/style/dark.css'
import { useEffect } from 'react'
import { logsOn } from '../../editors/logs/LogsObject.ts'
import { onOpenLogs } from '../../editors/logs/openLogs.ts'
import { fromSearch } from '../../editors/logs/view.ts'
import WorkbenchContextProvider from '../../workbench/context/WorkbenchContextProvider.tsx'
import { workbenchContext } from '../../workbench/context/WorkbenchContextStore.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { editors } from '../../workbench/points.ts'
import { session } from '../../session/session.ts'
import { onKindsRemoved, openDeepLink } from '../../ui/editorKinds.ts'
import { bordersJson, syncBorders } from './borders.ts'
import { createFactory } from './ComponentFactory.tsx'
import { EditorRegistry } from './EditorRegistry.ts'
import SideBar from './SideBar.tsx'

// Built on first render, after register.ts has loaded every contribution
let workbench: { model: Model; registry: EditorRegistry; factory: ReturnType<typeof createFactory> } | undefined

// The tabs of restorable editors reopen after a reload, from this browser's
// storage (per tenant and user). App tabs are restorable even before their
// kinds have loaded.
type SavedTab = { id: string; name: string; helpText?: string; component: string; config: unknown }

function savedTabsKey(): string {
  const account = session.get().account
  return account ? `workbench.tabs.${account.tenantId}.${account.user.id}` : 'workbench.tabs.local'
}

function restorable(kind: string): boolean {
  return kind.startsWith('app:') || !!extensions.get(editors).find((c) => c.id === kind)?.restorable
}

type SavedTabs = { tabs: SavedTab[]; selected?: string }

function loadTabs(): SavedTabs {
  try {
    const raw = localStorage.getItem(savedTabsKey())
    const saved = raw ? (JSON.parse(raw) as SavedTabs) : undefined
    return Array.isArray(saved?.tabs) ? saved : { tabs: [] }
  } catch {
    return { tabs: [] }
  }
}

function saveTabs(model: Model) {
  const tabs: SavedTab[] = []
  model.visitNodes((node) => {
    if (node instanceof TabNode && node.getParent() instanceof TabSetNode && restorable(node.getComponent() ?? '')) {
      tabs.push({ id: node.getId(), name: node.getName(), helpText: node.getHelpText(), component: node.getComponent() ?? '', config: node.getConfig() })
    }
  })
  const selected = model.getActiveTabset()?.getSelectedNode()?.getId()
  try {
    localStorage.setItem(savedTabsKey(), JSON.stringify({ tabs, selected } satisfies SavedTabs))
  } catch {
    // storage unavailable: tabs are not restored
  }
}

function restoredTabset() {
  const { tabs, selected } = loadTabs()
  return { children: tabs.map((t) => ({ type: 'tab' as const, ...t })), selected: Math.max(tabs.findIndex((t) => t.id === selected), 0) }
}

function getWorkbench() {
  if (!workbench) {
    const json: IJsonModel = {
      global: { tabSetEnableDeleteWhenEmpty: true },
      borders: bordersJson(),
      layout: {
        type: 'row',
        weight: 100,
        children: [{ type: 'tabset', weight: 100, ...restoredTabset() }],
      },
    }
    const model = Model.fromJson(json)
    const registry = new EditorRegistry(model)
    workbench = { model, registry, factory: createFactory(registry) }
  }
  return workbench
}

// Tells the workbench context store which editor tabs are open and which is
// focused, so focus tracking needs no editor code
function syncContext(model: Model) {
  const tabs: { tab: string; kind: string; title: string }[] = []
  model.visitNodes((node) => {
    if (node instanceof TabNode && node.getComponent() !== 'borderTab' && node.getParent() instanceof TabSetNode) {
      tabs.push({ tab: node.getId(), kind: node.getComponent() ?? '', title: node.getName() })
    }
  })
  workbenchContext.setLayout(tabs, model.getActiveTabset()?.getSelectedNode()?.getId())
}

function Workbench() {
  const { model, registry, factory } = getWorkbench()
  useEffect(() => extensions.subscribe(() => syncBorders(model)), [model])
  useEffect(() => {
    const listener = {
      onAfterAction: () => {
        syncContext(model)
        saveTabs(model)
      },
    }
    syncContext(model)
    model.addChangeListener(listener)
    return () => model.removeChangeListener(listener)
  }, [model])
  // Log Explorer deep links (/workbench?logs=<query>&range=1h) and requests from other views
  useEffect(() => {
    const linked = fromSearch(location.search)
    if (linked) registry.edit(logsOn(linked))
    return onOpenLogs((view) => registry.edit(logsOn(view)))
  }, [registry])
  // App deep links (/workbench?app=<appId>&route=/task/123)
  useEffect(() => openDeepLink(registry, location.search), [registry])
  // An app uninstalled, or an editor kind an upgrade removed: its tabs close
  useEffect(
    () =>
      onKindsRemoved((kinds) => {
        const gone: string[] = []
        model.visitNodes((node) => {
          if (node instanceof TabNode && kinds.includes(node.getComponent() ?? '')) gone.push(node.getId())
        })
        for (const id of gone) registry.close(id)
      }),
    [model, registry],
  )

  return (
    <WorkbenchContextProvider>
      <main style={{ display: 'flex', flexGrow: 1, minHeight: 0 }}>
        <SideBar registry={registry} />
        <div style={{ position: 'relative', flexGrow: 1 }}>
          <Layout model={model} factory={factory} />
        </div>
      </main>
    </WorkbenchContextProvider>
  )
}

export default Workbench
