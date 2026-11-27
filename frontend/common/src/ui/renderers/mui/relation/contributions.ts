// mui.relationPicker's renderer (forge 40b), in its own contributions so the
// shared renderer list is left alone
import { lazy } from 'react'
import { extensions } from '../../../../workbench/extensions/index.ts'
import { renderers } from '../../../points.ts'

extensions.contribute(renderers, { id: 'mui.relationPicker', source: 'ui', component: lazy(() => import('./RelationPicker.tsx')) })
