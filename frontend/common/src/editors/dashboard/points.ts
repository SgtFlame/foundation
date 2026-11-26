// Extension points owned by the Dashboard (documentation/Usage.md › Dashboard)

import type { ComponentType } from 'react'
import type { EditorRegistry } from '../../pages/workbench/EditorRegistry.ts'
import { defineExtensionPoint, type Contribution } from '../../workbench/extensions/index.ts'

export interface DashboardWidgetProps {
  // Changes when the Dashboard refreshes (on focus and every minute), so a
  // widget reloads its data
  refresh: number
  registry: EditorRegistry
}

// A widget on the Dashboard's grid; order places it
export interface DashboardWidgetContribution extends Contribution {
  title: string
  size: 'full' | 'half' // its default width on a wide screen
  component: ComponentType<DashboardWidgetProps>
}

export const dashboardWidgets = defineExtensionPoint<DashboardWidgetContribution>(
  'dashboard.widget',
  'Widgets on the AgentOS Dashboard, such as Usage & cost',
)
