import { lazy } from 'react'
import { extensions } from '../workbench/extensions/index.ts'
import { borderTabs } from '../workbench/points.ts'
import { loadServerSections } from './configStore.ts'

extensions.contribute(borderTabs, {
  id: 'settings',
  source: 'settings',
  order: 1000, // after DevTools
  name: 'Settings',
  helpText: 'Application settings, from every contributed section',
  location: 'right',
  size: 560,
  component: lazy(() => import('../tabs/settings/SettingsTab.tsx')),
})

void loadServerSections()
