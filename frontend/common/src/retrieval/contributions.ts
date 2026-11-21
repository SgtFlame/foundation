import MenuBookIcon from '@mui/icons-material/MenuBook'
import { chatContextSettings } from '../editors/chat/points.ts'
import { extensions } from '../workbench/extensions/index.ts'
import RagControl from './RagControl.tsx'

extensions.contribute(chatContextSettings, {
  id: 'rag',
  source: 'retrieval',
  order: 100,
  title: 'RAG Search',
  icon: MenuBookIcon,
  Control: RagControl,
})
