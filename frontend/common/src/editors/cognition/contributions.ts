import AccountTreeIcon from '@mui/icons-material/AccountTree'
import { lazy } from 'react'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors, sideViews } from '../../workbench/points.ts'
import { CognitionObject } from './CognitionObject.ts'

extensions.contribute(editors, {
  id: 'cognition',
  source: 'cognition',
  name: 'Cognition',
  component: lazy(() => import('./CognitionEditor.tsx')),
})

extensions.contribute(sideViews, {
  id: 'cognition',
  source: 'cognition',
  order: 300,
  title: 'Cognition',
  icon: AccountTreeIcon,
  width: 280,
  component: lazy(() => import('./ExplorerView.tsx')),
})

// Personal is the built-in chat mode; the Explorer side view lists every graph
extensions.contribute(commands, {
  id: 'open-cognition',
  source: 'cognition',
  order: 120,
  title: 'Cognition',
  icon: AccountTreeIcon,
  section: 'AgentOS',
  run: ({ registry }) => registry.edit(new CognitionObject({ id: 'personal', name: 'Personal' })),
})
