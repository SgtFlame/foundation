import { lazy } from 'react'
import { session } from '../session/session.ts'
import { extensions } from '../workbench/extensions/index.ts'
import { loadEditorKinds } from './editorKinds.ts'
import { renderers } from './points.ts'
import { muiRenderers } from './renderers/mui/index.ts'
import { starterRenderers } from './renderers/record/index.tsx'
import { HtmlView } from './renderers/doc/HtmlView.tsx'
import { ChipsInput } from './renderers/doc/ChipsInput.tsx'
import { MuiVirtualGrid, MuiVirtualList } from './renderers/doc/Windowed.tsx'
import { ChatSources } from './renderers/doc/ChatSources.tsx'

// The renderers of the primitives, by the server's component ids
// The content components (forge 07l)
const contentRenderers = { 'mui.htmlView': HtmlView, 'mui.chipsInput': ChipsInput, 'mui.virtualList': MuiVirtualList, 'mui.virtualGrid': MuiVirtualGrid, 'chat.sources': ChatSources }
for (const [id, component] of Object.entries({ ...muiRenderers, ...starterRenderers, ...contentRenderers })) {
  extensions.contribute(renderers, { id, source: 'ui', component })
}
extensions.contribute(renderers, { id: 'layout.flex', source: 'ui', component: lazy(() => import('./renderers/flexlayout/FlexLayout.tsx')) })
extensions.contribute(renderers, { id: 'doc.markdown', source: 'ui', component: lazy(() => import('./renderers/doc/DocMarkdown.tsx')) })
extensions.contribute(renderers, { id: 'doc.richText', source: 'ui', component: lazy(() => import('./renderers/doc/DocRichText.tsx')) })
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
