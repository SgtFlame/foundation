import type { TabNode } from 'flexlayout-react'
import BorderTabHost from './BorderTabHost.tsx'
import EditorHost from './EditorHost.tsx'
import type { EditorRegistry } from './EditorRegistry.ts'

// Border tabs render their workbench.borderTab contribution, and every other
// tab its object's workbench.editor contribution
export const createFactory = (registry: EditorRegistry) => (node: TabNode) => {
  if (node.getComponent() === 'borderTab') return <BorderTabHost id={node.getConfig().contributionId} />
  return <EditorHost object={node.getConfig().object} registry={registry} tabId={node.getId()} />
}
