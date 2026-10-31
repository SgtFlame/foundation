import { lazy } from 'react'
import { extensions } from '../../workbench/extensions/index.ts'
import { editors } from '../../workbench/points.ts'

extensions.contribute(editors, {
  id: 'chronicle',
  source: 'chronicle',
  name: 'Chronicle',
  component: lazy(() => import('./ChronicleEditor.tsx')),
})
