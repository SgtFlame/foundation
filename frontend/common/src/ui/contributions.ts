import { lazy } from 'react'
import { session } from '../session/session.ts'
import { extensions } from '../workbench/extensions/index.ts'
import { loadEditorKinds } from './editorKinds.ts'
import { renderers } from './points.ts'
import { muiRenderers } from './renderers/mui/index.ts'
import { starterRenderers } from './renderers/record/index.tsx'

// The renderers of the primitives, by the server's component ids
for (const [id, component] of Object.entries({ ...muiRenderers, ...starterRenderers })) {
  extensions.contribute(renderers, { id, source: 'ui', component })
}
extensions.contribute(renderers, { id: 'layout.flex', source: 'ui', component: lazy(() => import('./renderers/flexlayout/FlexLayout.tsx')) })
extensions.contribute(renderers, { id: 'graph.canvas', source: 'ui', component: lazy(() => import('./renderers/graph/GraphCanvas.tsx')) })

// The apps' editor kinds, followed once there is a session
let loaded = false
function loadWhenSignedIn() {
  const { status } = session.get()
  if (loaded || (status !== 'signedIn' && status !== 'singleUser')) return
  loaded = true
  loadEditorKinds()
}
session.subscribe(loadWhenSignedIn)
loadWhenSignedIn()
