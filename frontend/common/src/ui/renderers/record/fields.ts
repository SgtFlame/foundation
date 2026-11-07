// Entity type fields as entity.describe gives them, and the helpers the
// record.* renderers share (backend/features/records: Records.go,
// FieldTypes.go, Values.go)

export interface FieldOption {
  id: string
  name: string
  color?: string
}

export interface Field {
  id: string
  name: string
  type: string
  description?: string
  required?: boolean
  hidden?: boolean
  options?: FieldOption[]
  relation?: { entity: string; many?: boolean; inverse?: string }
  scale?: number
  currency?: string
  items?: Field
  fields?: Field[]
  returns?: string
}

export type RecordValues = Record<string, unknown>

// Types whose values the record's own events write, never a command
const computed = new Set(['formula', 'rollup', 'autoNumber', 'createdAt', 'updatedAt', 'createdBy'])

export function isComputed(f: Field): boolean {
  return computed.has(f.type)
}

// The field a computed value is shown as: a formula or rollup by what it
// returns, the automatic ones by their kind
export function shownAs(f: Field): Field {
  switch (f.type) {
    case 'formula':
    case 'rollup':
      return f.returns ? { ...f, type: f.returns } : { ...f, type: 'text' }
    case 'autoNumber':
      return { ...f, type: 'number' }
    case 'createdAt':
    case 'updatedAt':
      return { ...f, type: 'dateTime' }
    case 'createdBy':
      return { ...f, type: 'user' }
  }
  return f
}

// Whether two values are the same, as the server compares them (JSON)
export function same(a: unknown, b: unknown): boolean {
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null)
}

// The fields a form or a detail shows, in their order
export function visibleFields(fields: Field[] | undefined): Field[] {
  return (fields ?? []).filter((f) => !f.hidden)
}
