import { lazy } from 'react'
import { extensions } from '../../workbench/extensions/index.ts'
import { borderTabs } from '../../workbench/points.ts'

extensions.contribute(borderTabs, {
  id: 'devtools',
  source: 'devtools',
  order: 900, // before Settings
  name: 'DevTools',
  helpText: 'Captured backend traffic, the log, the extension registry and AI usage',
  location: 'right',
  size: 480,
  component: lazy(() => import('./DevToolsTab.tsx')),
})
