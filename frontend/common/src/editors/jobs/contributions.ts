import WorkHistoryIcon from '@mui/icons-material/WorkHistory'
import { lazy } from 'react'
import { JobsObject } from './JobsObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import { canIn } from '../../session/permissions.ts'

extensions.contribute(editors, {
  id: 'jobs',
  source: 'jobs',
  name: 'Jobs',
  component: lazy(() => import('./JobsEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-jobs',
  source: 'jobs',
  order: 545,
  title: 'Jobs',
  icon: WorkHistoryIcon,
  section: 'Administration',
  when: (ctx) => canIn(ctx, 'job.read'),
  run: ({ registry }) => registry.edit(new JobsObject()),
})
