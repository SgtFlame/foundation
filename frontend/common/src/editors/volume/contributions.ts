import { lazy } from 'react'
import { extensions } from '../../workbench/extensions/index.ts'
import { editors } from '../../workbench/points.ts'

extensions.contribute(editors, {
  id: 'volume',
  source: 'kb',
  name: 'Volume',
  component: lazy(() => import('./VolumeEditor.tsx')),
})
