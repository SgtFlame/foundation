import AdminPanelSettingsIcon from '@mui/icons-material/AdminPanelSettings'
import { lazy } from 'react'
import { RolesObject } from './RolesObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import { canIn } from '../../session/permissions.ts'

extensions.contribute(editors, {
  id: 'roles',
  source: 'authz',
  name: 'Roles',
  component: lazy(() => import('./RolesEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-roles',
  source: 'authz',
  order: 530,
  title: 'Roles',
  icon: AdminPanelSettingsIcon,
  section: 'Administration',
  when: (ctx) => canIn(ctx, 'role.read'),
  run: ({ registry }) => registry.edit(new RolesObject()),
})
