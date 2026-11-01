import { lazy } from 'react'
import { session } from '../session/session.ts'
import { extensions } from '../workbench/extensions/index.ts'
import { loadEditorKinds } from './editorKinds.ts'
import { renderers } from './points.ts'
import { muiRenderers } from './renderers/mui/index.ts'

// The renderers of the primitives, by the server's component ids
for (const [id, component] of Object.entries(muiRenderers)) {
  extensions.contribute(renderers, { id, source: 'ui', component })
}
extensions.contribute(renderers, { id: 'layout.flex', source: 'ui', component: lazy(() => import('./renderers/flexlayout/FlexLayout.tsx')) })
extensions.contribute(renderers, { id: 'graph.canvas', source: 'ui', component: lazy(() => import('./renderers/graph/GraphCanvas.tsx')) })

// The installed apps' editor kinds, once there is a session
let loaded = false
function loadWhenSignedIn() {
  const { status } = session.get()
  if (loaded || (status !== 'signedIn' && status !== 'singleUser')) return
  loaded = true
  void loadEditorKinds()
}
session.subscribe(loadWhenSignedIn)
loadWhenSignedIn()
