import KeyIcon from '@mui/icons-material/Key'
import { lazy } from 'react'
import { SecretsObject } from './SecretsObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import { canIn } from '../../session/permissions.ts'

extensions.contribute(editors, {
  id: 'secrets',
  source: 'secrets',
  name: 'Secrets',
  component: lazy(() => import('./SecretsEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-secrets',
  source: 'secrets',
  order: 545,
  title: 'Secrets',
  icon: KeyIcon,
  section: 'Administration',
  when: (ctx) => canIn(ctx, 'secret.list', 'app'),
  run: ({ registry }) => registry.edit(new SecretsObject()),
})
