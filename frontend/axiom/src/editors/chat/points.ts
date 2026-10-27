// Extension points owned by the chat surfaces (documentation/plans/ChatCompletion.md)

import type { ComponentType } from 'react'
import type { EditorRegistry } from '../../pages/workbench/EditorRegistry.ts'
import type { ToolCall } from '../../services/Conversation.ts'
import type { WorkbenchContextStore } from '../../workbench/context/WorkbenchContextStore.ts'
import { defineExtensionPoint, type Contribution } from '../../workbench/extensions/index.ts'

export type ChatSurface = 'chat' | 'document'

export interface ContextControlProps {
  value: Record<string, unknown> // the conversation's settings for this contribution
  onChange: (value: Record<string, unknown>) => void
}

// A control in the composer's ⚙ settings popover. Its id pairs it with the
// server's chat.contextProvider of the same id, which reads the settings;
// the popover hides a control the server has no provider for. Defaults are
// the server's chat.context.defaults.<id> section, edited in Settings.
export interface ChatContextSettingsContribution extends Contribution {
  title: string
  icon?: ComponentType<{ fontSize?: 'small' }>
  Control: ComponentType<ContextControlProps>
  // when(ctx) sees { surface: ChatSurface }
}

export const chatContextSettings = defineExtensionPoint<ChatContextSettingsContribution>(
  'chat.contextSettings',
  'Controls in the chat settings popover, each paired with a server chat.contextProvider',
)

// What a /command can do to the chat it was typed in
export interface ChatCommandContext {
  newConversation: () => void
  setLevel: (id: string) => boolean // false when there is no such level
  levels: readonly { id: string; label: string }[]
  say: (markdown: string) => void // a note in the transcript, not sent to the model
  commands: readonly ChatCommandContribution[]
}

// A /command typed in the composer; the id is the command's name
export interface ChatCommandContribution extends Contribution {
  description: string
  usage?: string // e.g. '<level>'
  run: (arg: string, ctx: ChatCommandContext) => void
}

export const chatCommands = defineExtensionPoint<ChatCommandContribution>(
  'chat.command',
  'Slash commands in the chat composer, such as /new and /level',
)

// What a client tool runs with: the workbench it may read and act on
export interface ClientToolContext {
  registry: EditorRegistry
  store: WorkbenchContextStore
  tabId: string // the chat surface's tab
}

// The card of a client tool that asks the user, such as chat.askChoice
export interface ClientToolCardProps {
  call: ToolCall
  answer: (result: Record<string, unknown>) => void
}

// A tool the model calls that runs in this browser
// (documentation/plans/WorkbenchContext.md › Client tools). The id is the
// tool's name. It may only read the workbench or change what the user sees
// (ui); changing data is for server tools, which ask first. A tool with a
// Card waits for the user to answer it there instead of running.
export interface ChatClientToolContribution extends Contribution {
  title: string
  description: string // for the model
  input?: Record<string, unknown> // the arguments' JSON Schema
  effect: 'read' | 'ui'
  run?: (args: Record<string, unknown>, ctx: ClientToolContext) => Promise<Record<string, unknown>> | Record<string, unknown>
  Card?: ComponentType<ClientToolCardProps>
  // when(ctx) sees { surface: ChatSurface }
}

export const chatClientTools = defineExtensionPoint<ChatClientToolContribution>(
  'chat.clientTool',
  'Tools the model calls that run in the browser, such as workbench.open and chat.askChoice',
)
