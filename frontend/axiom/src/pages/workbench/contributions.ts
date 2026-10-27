import { lazy } from 'react'
import AppsIcon from '@mui/icons-material/Apps'
import { extensions } from '../../workbench/extensions/index.ts'
import { sideViews } from '../../workbench/points.ts'

extensions.contribute(sideViews, {
  id: 'editors',
  source: 'workbench',
  order: 100,
  title: 'Editors',
  icon: AppsIcon,
  width: 220,
  component: lazy(() => import('./EditorsView.tsx')),
})
