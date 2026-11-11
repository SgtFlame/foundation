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

// The node types the Palette makes from a contract
export const callNode = 'connector.call'
export const onNode = 'connector.on'
