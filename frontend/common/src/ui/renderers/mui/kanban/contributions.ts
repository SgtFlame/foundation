// mui.kanban's renderer (forge 30b), in its own contributions so the
// shared renderer list is left alone
import { lazy } from 'react'
import { extensions } from '../../../../workbench/extensions/index.ts'
import { renderers } from '../../../points.ts'

extensions.contribute(renderers, { id: 'mui.kanban', source: 'ui', component: lazy(() => import('./Kanban.tsx')) })
