// Extension points owned by the server-driven UI renderer
import type { ComponentType, ReactNode } from 'react'
import { defineExtensionPoint, type Contribution } from '../workbench/extensions/index.ts'
import type { UiNode } from './types.ts'
import type { UiSession } from './UiSession.ts'

// What a primitive's renderer gets
export interface RendererProps {
  node: UiNode
  path: string[] // the node's key path, for its events
  // The node's props with its local.* bindings resolved
  props: Record<string, unknown>
  session: UiSession
  // Sends one of the node's events, when the tree maps it to a handler
  emit(event: string, data?: Record<string, unknown>): void
  // Whether the tree maps an event to a handler
  handles(event: string): boolean
  // Sets a prop bound to local state (a typed value); false when the prop
  // is not bound locally
  setLocal(prop: string, value: unknown): boolean
  // The rendered children, in order
  children: ReactNode[]
  // Renders one child by key (lazily rendered tabs, graph node bodies)
  renderChild(key: string): ReactNode
  // The rendered contents of a named slot
  slot(name: string): ReactNode[]
}

// The browser half of a primitive: its id is the component type on the
// server's ui.component point, e.g. "mui.button"
export interface RendererContribution extends Contribution {
  component: ComponentType<RendererProps>
}

export const renderers = defineExtensionPoint<RendererContribution>(
  'ui.renderer',
  'Renderers of server-described UI components, by component type (mui.*, layout.flex, graph.canvas)',
)
