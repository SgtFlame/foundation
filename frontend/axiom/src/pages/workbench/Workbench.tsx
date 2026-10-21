import { Layout, Model, type IJsonModel } from 'flexlayout-react'
import 'flexlayout-react/style/light.css'
import { createFactory } from './ComponentFactory.tsx'
import { EditorRegistry } from './EditorRegistry.ts'
import LeftMenuSidebar from './LeftMenuSidebar.tsx'

const json: IJsonModel = {
  global: { tabSetEnableDeleteWhenEmpty: true },
  borders: [
    {
      type: 'border',
      location: 'right',
      selected: -1,
      size: 480,
      children: [
        {
          type: 'tab',
          id: 'devtools',
          name: 'DevTools',
          component: 'devtools',
          enableClose: false,
        },
      ],
    },
  ],
  layout: {
    type: 'row',
    weight: 100,
    children: [{ type: 'tabset', weight: 100, children: [] }],
  },
}

const model = Model.fromJson(json)
const registry = new EditorRegistry(model)
const factory = createFactory(registry)

function Workbench() {
  return (
    <main style={{ display: 'flex', flexGrow: 1 }}>
      <LeftMenuSidebar registry={registry} />
      <div style={{ position: 'relative', flexGrow: 1 }}>
        <Layout model={model} factory={factory} />
      </div>
    </main>
  )
}

export default Workbench
