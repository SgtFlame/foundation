import { Layout, Model, type IJsonModel, type TabNode } from 'flexlayout-react'
import ExtensionsViewer from './extensions/ExtensionsViewer.tsx'
import LogViewer from './log/LogViewer.tsx'
import NetworkViewer from './network/NetworkViewer.tsx'
import UsageViewer from './usage/UsageViewer.tsx'

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
          { type: 'tab', id: 'devtools:extensions', name: 'Extensions', component: 'extensions' },
          { type: 'tab', id: 'devtools:usage', name: 'Usage', component: 'usage' },
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
    case 'extensions':
      return <ExtensionsViewer />
    case 'usage':
      return <UsageViewer />
    default:
      return null
  }
}

// Developer tools: captured backend traffic, the structured log, the extension
// registry and the recorded AI usage
function DevToolsTab() {
  return (
    <div style={{ position: 'relative', width: '100%', height: '100%' }}>
      <Layout model={model} factory={factory} />
    </div>
  )
}

export default DevToolsTab
