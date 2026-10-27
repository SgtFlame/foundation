// Extension points owned by the workbench

import type { ComponentType } from 'react'
import type { EditorRegistry } from '../pages/workbench/EditorRegistry.ts'
import { defineExtensionPoint, type Contribution } from './extensions/index.ts'

export type BorderLocation = 'left' | 'right' | 'top' | 'bottom'

// A tab in one of the workbench's borders, collapsed until clicked
export interface BorderTabContribution extends Contribution {
  name: string
  helpText?: string
  location: BorderLocation
  size?: number // the border's size when open, in pixels; the largest of its tabs wins
  component: ComponentType // usually lazy(() => import(...)), so discovery stays light
}

export const borderTabs = defineExtensionPoint<BorderTabContribution>(
  'workbench.borderTab',
  'Tabs in the workbench borders, such as DevTools and Settings',
)

// What an editor tab edits: the object EditorRegistry.edit was given. An
// object may set its own tab text (name) and tab hover text (helpText).
export interface EditableObject {
  readonly kind: string // the workbench.editor contribution that edits it
  readonly id: string
  readonly name?: string
  readonly helpText?: string
}

export interface EditorProps<T extends EditableObject = EditableObject> {
  object: T
  registry: EditorRegistry // for editors that open other editors
  tabId: string // `<kind>:<id>`, e.g. for useWorkbenchSubject
}

// The editor for one kind of object; the contribution's id is the kind
export interface EditorContribution extends Contribution {
  name: string // e.g. 'Markdown'; a tab whose object has no name is '<name> <id>'
  // Usually lazy(() => import(...)). Typed for any object; the host passes
  // the tab's object, of the contribution's kind.
  component: ComponentType<EditorProps<never>>
}

export const editors = defineExtensionPoint<EditorContribution>(
  'workbench.editor',
  'Editors for the kinds of object a workbench tab can edit, such as Markdown and Chat',
)

// What a side view or a command gets: the registry, to open editors
export interface WorkbenchContext {
  registry: EditorRegistry
}

// A view in the workbench's left side bar, chosen from its vertical tabs
// (an activity bar); clicking the open view's tab hides the side bar
export interface SideViewContribution extends Contribution {
  title: string
  icon: ComponentType<{ fontSize?: 'small' | 'medium' }>
  width?: number // the side bar's width when this view is open, in pixels
  component: ComponentType<WorkbenchContext> // usually lazy(() => import(...))
}

export const sideViews = defineExtensionPoint<SideViewContribution>(
  'workbench.sideView',
  'Views in the left side bar, such as Editors and Chronicles',
)

// A command, such as opening an editor. A command with a section is listed
// in the Editors side view under that section.
export interface CommandContribution extends Contribution {
  title: string
  icon?: ComponentType<{ fontSize?: 'small' | 'medium' }>
  section?: string // e.g. 'AgentOS', 'Knowledge'
  run: (ctx: WorkbenchContext) => void
}

export const commands = defineExtensionPoint<CommandContribution>(
  'workbench.command',
  'Commands, such as opening an editor; those with a section are listed in the Editors side view',
)
