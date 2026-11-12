import WorkspacesIcon from '@mui/icons-material/Workspaces'
import { chatContextSettings } from '../../editors/chat/points.ts'
import { extensions } from '../extensions/index.ts'
import ContextControl from './ContextControl.tsx'

extensions.contribute(chatContextSettings, {
  id: 'workbench',
  source: 'workbench',
  order: 200,
  title: 'Context',
  icon: WorkspacesIcon,
  Control: ContextControl,
})
