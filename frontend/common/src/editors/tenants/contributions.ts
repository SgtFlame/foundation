import ApartmentIcon from '@mui/icons-material/Apartment'
import { lazy } from 'react'
import { TenantsObject } from './TenantsObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import { canIn } from '../../session/permissions.ts'

extensions.contribute(editors, {
  id: 'tenants',
  source: 'identity',
  name: 'Tenants',
  component: lazy(() => import('./TenantsEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-tenants',
  source: 'identity',
  order: 510,
  title: 'Tenants',
  icon: ApartmentIcon,
  section: 'Administration',
  when: (ctx) => canIn(ctx, 'tenant.list', 'app') || canIn(ctx, 'tenant.addMember'),
  run: ({ registry }) => registry.edit(new TenantsObject()),
})
