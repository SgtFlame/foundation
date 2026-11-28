import { lazy } from 'react'
import { extensions } from '../../workbench/extensions/index.ts'
import { editors } from '../../workbench/points.ts'

extensions.contribute(editors, {
  id: 'richtext',
  source: 'kb',
  name: 'Document',
  component: lazy(() => import('./RichTextEditor.tsx')),
})
