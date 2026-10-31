import GroupIcon from '@mui/icons-material/Group'
import { lazy } from 'react'
import { UsersObject } from './UsersObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import { canIn } from '../../session/permissions.ts'

extensions.contribute(editors, {
  id: 'users',
  source: 'identity',
  name: 'Users',
  component: lazy(() => import('./UsersEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-users',
  source: 'identity',
  order: 520,
  title: 'Users',
  icon: GroupIcon,
  section: 'Administration',
  when: (ctx) => canIn(ctx, 'user.list', 'app'),
  run: ({ registry }) => registry.edit(new UsersObject()),
})
