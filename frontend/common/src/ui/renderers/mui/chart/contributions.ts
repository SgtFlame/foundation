// mui.chart's renderer (forge 40a), in its own contributions so the shared
// renderer list is left alone; Recharts loads with it
import { lazy } from 'react'
import { extensions } from '../../../../workbench/extensions/index.ts'
import { renderers } from '../../../points.ts'

extensions.contribute(renderers, { id: 'mui.chart', source: 'ui', component: lazy(() => import('./Chart.tsx')) })
extensions.contribute(renderers, { id: 'mui.metric', source: 'ui', component: lazy(() => import('./Metric.tsx')) })
