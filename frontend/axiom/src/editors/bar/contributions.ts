import BarChartIcon from '@mui/icons-material/BarChart'
import { lazy } from 'react'
import { BarObject } from './BarObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'

extensions.contribute(editors, {
  id: 'bar',
  source: 'bar',
  name: 'Bar',
  component: lazy(() => import('./BarEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-bar',
  source: 'bar',
  order: 300,
  title: 'Bar',
  icon: BarChartIcon,
  section: 'Two',
  run: ({ registry }) => registry.edit(new BarObject('bar-1')),
})
