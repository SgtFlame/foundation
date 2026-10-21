import { Layout, Model, type IJsonModel, type TabNode } from 'flexlayout-react'
import LogViewer from './log/LogViewer.tsx'
import NetworkViewer from './network/NetworkViewer.tsx'

const json: IJsonModel = {
  global: { tabEnableClose: false, tabSetEnableMaximize: false },
  borders: [],
  layout: {
    type: 'row',
    weight: 100,
    children: [
      {
        type: 'tabset',
        weight: 100,
        children: [
          { type: 'tab', id: 'devtools:network', name: 'Network', component: 'network' },
          { type: 'tab', id: 'devtools:log', name: 'Log', component: 'log' },
        ],
      },
    ],
  },
}

const model = Model.fromJson(json)

function factory(node: TabNode) {
  switch (node.getComponent()) {
    case 'network':
      return <NetworkViewer />
    case 'log':
      return <LogViewer />
    default:
      return null
  }
}

// Developer tools: captured backend traffic and the structured log
function DevToolsTab() {
  return (
    <div style={{ position: 'relative', width: '100%', height: '100%' }}>
      <Layout model={model} factory={factory} />
    </div>
  )
}

export default DevToolsTab
