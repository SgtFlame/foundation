import ManageSearchIcon from '@mui/icons-material/ManageSearch'
import { lazy } from 'react'
import { LogsObject } from './LogsObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import { canIn } from '../../session/permissions.ts'

extensions.contribute(editors, {
  id: 'logs',
  source: 'logs',
  name: 'Log Explorer',
  component: lazy(() => import('./LogsEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-logs',
  source: 'logs',
  order: 500,
  title: 'Log Explorer',
  icon: ManageSearchIcon,
  section: 'Administration',
  when: (ctx) => canIn(ctx, 'logs.read'),
  run: ({ registry }) => registry.edit(new LogsObject('explorer')),
})
