// mui.calendar's renderer (forge 28d), in its own contributions so the
// shared renderer list is left alone
import { lazy } from 'react'
import { extensions } from '../../../../workbench/extensions/index.ts'
import { renderers } from '../../../points.ts'

extensions.contribute(renderers, { id: 'mui.calendar', source: 'ui', component: lazy(() => import('./Calendar.tsx')) })
