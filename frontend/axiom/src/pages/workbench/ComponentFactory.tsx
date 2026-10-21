import type { TabNode } from 'flexlayout-react'
import BarEditor from '../../editors/bar/BarEditor.tsx'
import FooEditor from '../../editors/foo/FooEditor.tsx'
import MarkdownEditor from '../../editors/markdown/MarkdownEditor.tsx'
import VolumeEditor from '../../editors/volume/VolumeEditor.tsx'
import VolumesEditor from '../../editors/volumes/VolumesEditor.tsx'
import DevToolsTab from '../../tabs/devtools/DevToolsTab.tsx'
import type { EditorRegistry } from './EditorRegistry.ts'

// Editors that open other editors are given the registry
export const createFactory = (registry: EditorRegistry) => (node: TabNode) => {
  switch (node.getComponent()) {
    case 'foo':
      return <FooEditor object={node.getConfig().object} />
    case 'bar':
      return <BarEditor object={node.getConfig().object} />
    case 'volumes':
      return <VolumesEditor object={node.getConfig().object} registry={registry} />
    case 'volume':
      return <VolumeEditor object={node.getConfig().object} registry={registry} />
    case 'markdown':
      return <MarkdownEditor object={node.getConfig().object} />
    case 'devtools':
      return <DevToolsTab />
    default:
      return null
  }
}
