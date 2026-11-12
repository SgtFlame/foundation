import GavelIcon from '@mui/icons-material/Gavel'
import { lazy } from 'react'
import { ModerationObject } from './ModerationObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'
import { canIn } from '../../session/permissions.ts'

extensions.contribute(editors, {
  id: 'moderation',
  source: 'chronicle',
  name: 'Moderation',
  component: lazy(() => import('./ModerationEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-moderation',
  source: 'chronicle',
  order: 540,
  title: 'Moderation',
  icon: GavelIcon,
  section: 'Administration',
  when: (ctx) => canIn(ctx, 'chronicle.moderate') || canIn(ctx, 'chronicle.moderateGlobal', 'app'),
  run: ({ registry }) => registry.edit(new ModerationObject()),
})
