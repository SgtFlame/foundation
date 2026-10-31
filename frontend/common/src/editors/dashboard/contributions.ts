import DashboardIcon from '@mui/icons-material/Dashboard'
import { lazy } from 'react'
import { DashboardObject } from './DashboardObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import { dashboardWidgets } from './points.ts'

extensions.contribute(editors, {
  id: 'dashboard',
  source: 'dashboard',
  name: 'Dashboard',
  component: lazy(() => import('./DashboardEditor.tsx')),
})

extensions.contribute(dashboardWidgets, {
  id: 'usage',
  source: 'usage',
  order: 100,
  title: 'Usage & cost',
  size: 'full',
  component: lazy(() => import('./usage/UsageWidget.tsx')),
})

extensions.contribute(commands, {
  id: 'open-dashboard',
  source: 'dashboard',
  order: 110,
  title: 'Dashboard',
  icon: DashboardIcon,
  section: 'AgentOS',
  run: ({ registry }) => registry.edit(new DashboardObject()),
})
