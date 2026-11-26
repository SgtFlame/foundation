import { lazy } from 'react'
import AutoStoriesIcon from '@mui/icons-material/AutoStories'
import { chatContextSettings } from '../editors/chat/points.ts'
import { extensions } from '../workbench/extensions/index.ts'
import { commands, sideViews } from '../workbench/points.ts'
import { ChronicleObject } from '../editors/chronicle/ChronicleObject.ts'
import ChronicleControl from './ChronicleControl.tsx'
import { contextViewId } from './chatFocus.ts'

// The workspace's active Volumes and Chronicle entries (forge 16c: once the
// Chronicles view)
extensions.contribute(sideViews, {
  id: contextViewId,
  source: 'chronicle',
  order: 200,
  title: 'Context',
  icon: AutoStoriesIcon,
  width: 300,
  component: lazy(() => import('./ContextView.tsx')),
})

extensions.contribute(commands, {
  id: 'new-chronicle',
  source: 'chronicle',
  order: 410,
  title: 'New Chronicle',
  icon: AutoStoriesIcon,
  section: 'Knowledge',
  run: ({ registry }) => registry.edit(new ChronicleObject()),
})

extensions.contribute(chatContextSettings, {
  id: 'chronicle',
  source: 'chronicle',
  order: 50,
  title: 'Chronicles',
  icon: AutoStoriesIcon,
  Control: ChronicleControl,
})
