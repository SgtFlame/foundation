// Client for the backend UI REST handlers (backend/features/ui/rest/uihandlers).
// UI sessions themselves are ui.* frames on the live connection
// (src/ui/UiSession.ts).

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/ui/rest/uidto

export interface ComponentEvent {
  name: string
  description: string
}

export interface ComponentInfo {
  id: string
  title: string
  description: string
  category: string
  tags?: string[]
  props: unknown // JSON Schema
  events: ComponentEvent[]
  slots?: string[]
  children: boolean
  examples: unknown[]
}

export interface ComponentsResponse extends ResponseStatus {
  components: ComponentInfo[]
}

// What a user needs to use an app: an action on a resource below the
// tenant ("app:tasks/entity:task"), or on the tenant when it has none
export interface AppReach {
  action: string
  resource?: string
}

export interface AppInfo {
  id: string
  title: string
  description: string
  icon?: string
  reach: AppReach[] // any one of them; none: everyone who may read the UI
}

// A kind of top-level app tab; id is "app:<appId>:<kind>"
export interface EditorKindInfo {
  id: string
  app: string
  kind: string
  title: string
  icon?: string
  page: string // "<appId>/<page>"
}

export interface EditorKindsResponse extends ResponseStatus {
  apps: AppInfo[]
  kinds: EditorKindInfo[]
}

export const Ui = {
  async components(): Promise<ComponentsResponse> {
    const response = await http.get<ComponentsResponse>('/ui/components')
    return response.data
  },

  async editorKinds(): Promise<EditorKindsResponse> {
    const response = await http.get<EditorKindsResponse>('/ui/editor-kinds')
    return response.data
  },
}
