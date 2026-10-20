import type { TabNode } from 'flexlayout-react'
import BarEditor from '../../editors/bar/BarEditor.tsx'
import FooEditor from '../../editors/foo/FooEditor.tsx'
import VolumesEditor from '../../editors/volumes/VolumesEditor.tsx'

export function factory(node: TabNode) {
  switch (node.getComponent()) {
    case 'foo':
      return <FooEditor object={node.getConfig().object} />
    case 'bar':
      return <BarEditor object={node.getConfig().object} />
    case 'volumes':
      return <VolumesEditor object={node.getConfig().object} />
    default:
      return null
  }
}
