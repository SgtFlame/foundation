// The connector registry's types (forge 07b), as its live queries give
// them: useQuery<ContractList>('connectors.contracts', {search?}),
// useQuery<TypeList>('connectors.types', {contract?, search?, direction?})
// and useQuery('connectors.describe', {ref}).

export interface ConnectorMeta {
  title: string
  description: string
  tags: string[]
  icon?: string
}

export interface ConnectorExample {
  title?: string
  args: unknown
  result: unknown
}

export interface ConnectorOperation {
  id: string
  title: string
  description: string
  args: unknown // JSON Schema, an object
  result: unknown // JSON Schema, an object
  examples: ConnectorExample[]
  effect: 'read' | 'write' | 'send'
  destructive?: boolean
  key?: string
  errors?: string[]
  callbacks?: { event: string; correlate: { result: string; event: string } }[]
  remote?: string
}

export interface ConnectorEvent {
  id: string
  title: string
  description: string
  payload: unknown
  example: unknown
  idFields: string[]
  delivery: ('watch' | 'callback')[]
  subscription?: string
}

export interface Contract {
  id: string
  version: number
  meta: ConnectorMeta
  operations: ConnectorOperation[]
  events?: ConnectorEvent[]
}

export interface ContractInfo {
  contract: Contract
  ref: string // id@version
  source: 'builtin' | 'announced' | 'discovered'
  types: string[]
  online: boolean
  needsConnection: boolean
}

export interface ContractList {
  contracts: ContractInfo[]
}

export interface ConnectorType {
  id: string
  contracts: string[]
  meta: ConnectorMeta
  settings?: unknown
  noConnection?: boolean
  source: 'builtin' | 'scripted'
  discovers?: boolean
  receives?: string
  exclusive?: boolean
}

export interface TypeInfo {
  type: ConnectorType
  source: string
  direction: 'outbound'
  online: boolean
  instances: number
}

export interface SurfaceEntry {
  surface: { id: string; meta: ConnectorMeta }
  direction: 'inbound'
  online: boolean
  instances: number
}

export interface TypeList {
  types: TypeInfo[]
  surfaces: SurfaceEntry[]
}

// Connections (forge 07c): useQuery<ConnectionList>('connections.list',
// {type?, contract?}), useQuery<ConnectionDescription>('connections.describe',
// {id}), sent by connection.* commands (create and update with
// {secret: true}) and tested by the connection.test call. A secret is
// only ever {set}.
export type ConnectionStatus = 'ok' | 'error' | 'disabled'

export interface ConnectionOptions {
  pollInterval?: string
  since?: string // YYYY-MM-DD
  paused?: boolean
}

export interface Connection {
  id: string
  name: string
  type: string
  owner: string // tenant, or user:<id>
  settings: Record<string, unknown>
  secrets: Record<string, { set: boolean }>
  status: ConnectionStatus
  lastError?: string
  checkedAt?: string
  options: ConnectionOptions
  createdAt: string
  updatedAt: string
}

export interface ConnectionList {
  connections: Connection[]
}

export interface ConnectionDescription {
  connection: Connection | null
  type?: TypeInfo
}

export interface ConnectionTestResult {
  ok: boolean
  error?: { code: string; message: string; retryable: boolean }
}

// The Jobs view's event sources: useQuery<SourceList>('connectors.sources', {})
export interface EventSource {
  connection: string
  name: string
  type: string
  contracts: string[]
  online: boolean
  holder?: { adapter: string; instance: string; host: string; since: string; lastError?: string; lastErrorAt?: string }
  cursorAt?: string
  lastEvent?: string
}

export interface SourceList {
  sources: EventSource[]
}

// The node types the Palette makes from a contract
export const callNode = 'connector.call'
export const onNode = 'connector.on'
