import { Layout, Model, type IJsonModel } from 'flexlayout-react'
import 'flexlayout-react/style/light.css'
import { factory } from './ComponentFactory.tsx'
import { EditorRegistry } from './EditorRegistry.ts'
import LeftMenuSidebar from './LeftMenuSidebar.tsx'

const json: IJsonModel = {
  global: { tabSetEnableDeleteWhenEmpty: true },
  borders: [],
  layout: {
    type: 'row',
    weight: 100,
    children: [{ type: 'tabset', weight: 100, children: [] }],
  },
}

const model = Model.fromJson(json)
const registry = new EditorRegistry(model)

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
