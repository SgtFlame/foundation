import { lazy } from 'react'
import { extensions } from '../../workbench/extensions/index.ts'
import { editors } from '../../workbench/points.ts'

// Opened from the account menu at the bottom of the side bar
extensions.contribute(editors, {
  id: 'account',
  source: 'identity',
  name: 'Account',
  component: lazy(() => import('./AccountEditor.tsx')),
})
