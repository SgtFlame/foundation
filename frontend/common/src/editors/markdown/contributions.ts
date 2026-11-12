import { lazy } from 'react'
import { extensions } from '../../workbench/extensions/index.ts'
import { editors } from '../../workbench/points.ts'

extensions.contribute(editors, {
  id: 'markdown',
  source: 'kb',
  name: 'Markdown',
  component: lazy(() => import('./MarkdownEditor.tsx')),
})
