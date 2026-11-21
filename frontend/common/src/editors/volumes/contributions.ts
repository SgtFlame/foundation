import LibraryBooksIcon from '@mui/icons-material/LibraryBooks'
import { lazy } from 'react'
import { VolumesObject } from './VolumesObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'

extensions.contribute(editors, {
  id: 'volumes',
  source: 'kb',
  name: 'Volumes',
  component: lazy(() => import('./VolumesEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-volumes',
  source: 'kb',
  order: 400,
  title: 'Volumes',
  icon: LibraryBooksIcon,
  section: 'Knowledge',
  // Volumes are the default tenant's until each tenant has its own
  when: (ctx) => ctx.tenantId === '1',
  run: ({ registry }) => registry.edit(new VolumesObject('volumes')),
})
