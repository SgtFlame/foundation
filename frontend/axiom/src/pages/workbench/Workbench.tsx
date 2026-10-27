import { Layout, Model, TabNode, TabSetNode, type IJsonModel } from 'flexlayout-react'
import 'flexlayout-react/style/light.css'
import { useEffect } from 'react'
import { logsOn } from '../../editors/logs/LogsObject.ts'
import { onOpenLogs } from '../../editors/logs/openLogs.ts'
import { fromSearch } from '../../editors/logs/view.ts'
import WorkbenchContextProvider from '../../workbench/context/WorkbenchContextProvider.tsx'
import { workbenchContext } from '../../workbench/context/WorkbenchContextStore.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { bordersJson, syncBorders } from './borders.ts'
import { createFactory } from './ComponentFactory.tsx'
import { EditorRegistry } from './EditorRegistry.ts'
import SideBar from './SideBar.tsx'

// Built on first render, after main.tsx has loaded every contribution
let workbench: { model: Model; registry: EditorRegistry; factory: ReturnType<typeof createFactory> } | undefined

function getWorkbench() {
  if (!workbench) {
    const json: IJsonModel = {
      global: { tabSetEnableDeleteWhenEmpty: true },
      borders: bordersJson(),
      layout: {
        type: 'row',
        weight: 100,
        children: [{ type: 'tabset', weight: 100, children: [] }],
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
    const listener = { onAfterAction: () => syncContext(model) }
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

  return (
    <WorkbenchContextProvider>
      <main style={{ display: 'flex', flexGrow: 1 }}>
        <SideBar registry={registry} />
        <div style={{ position: 'relative', flexGrow: 1 }}>
          <Layout model={model} factory={factory} />
        </div>
      </main>
    </WorkbenchContextProvider>
  )
}

export default Workbench
