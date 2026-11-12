// The JSON-Schema subset settings sections are declared in, mirroring
// backend/features/config/Schema.go, and the helpers the Settings forms use
// on decoded values. Paths are dotted, array items by index ('models.0.id').

import type { Scope, SecretView } from '../services/Config.ts'

export type SchemaType = 'string' | 'number' | 'integer' | 'boolean' | 'enum' | 'secret' | 'object' | 'array'

export interface Schema {
  type: SchemaType
  title?: string
  description?: string
  properties?: Record<string, Schema> // object, in display order
  required?: string[] // object
  items?: Schema // array
  key?: string // array of objects: the property unique per item ('id' when absent)
  options?: { value: string; label?: string }[] // enum
  min?: number
  max?: number
  step?: number
  pattern?: string
  format?: 'url' | 'multiline' | 'currency' | 'duration'
  unit?: string
  scopes?: Scope[]
  restart?: boolean
  ref?: string // '<sectionId>#<arrayPath>': names an item of another section's array
}

export type Value = Record<string, unknown>

export function join(path: string, key: string | number): string {
  return path === '' ? String(key) : `${path}.${key}`
}

function segments(path: string): string[] {
  return path === '' ? [] : path.split('.')
}

export function getAt(value: unknown, path: string): unknown {
  let cur = value
  for (const seg of segments(path)) {
    if (cur === null || typeof cur !== 'object') return undefined
    cur = (cur as Record<string, unknown>)[seg]
  }
  return cur
}

// A copy of value with path set; arrays are copied as arrays
export function setAt<T>(value: T, path: string, next: unknown): T {
  const [head, ...rest] = segments(path)
  if (head === undefined) return next as T
  const container: Record<string, unknown> | unknown[] = Array.isArray(value)
    ? [...value]
    : { ...((value ?? {}) as Record<string, unknown>) }
  const child = (container as Record<string, unknown>)[head]
  const updated = rest.length === 0 ? next : setAt(child, rest.join('.'), next)
  if (Array.isArray(container)) container[Number(head)] = updated
  else if (updated === undefined) delete container[head]
  else container[head] = updated
  return container as T
}

// The settings that are not objects, under schema; arrays are leaves, edited
// and inherited as a whole
export function leaves(schema: Schema, path = ''): { path: string; schema: Schema }[] {
  if (schema.type !== 'object') return [{ path, schema }]
  return Object.entries(schema.properties ?? {}).flatMap(([k, s]) => leaves(s, join(path, k)))
}

export function isSecretView(value: unknown): value is SecretView {
  return value !== null && typeof value === 'object' && 'set' in value
}

export function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  const ka = Object.keys(a)
  const kb = Object.keys(b)
  return (
    ka.length === kb.length &&
    ka.every((k) => deepEqual((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]))
  )
}

// The merge patch turning original into edited, plus null for each reset
// path, so it is inherited again. A secret is only sent when replaced (a
// string) or cleared (null).
export function patchFrom(schema: Schema, original: Value, edited: Value, resets: ReadonlySet<string>): Value {
  let patch: Value = {}
  for (const leaf of leaves(schema)) {
    if (resets.has(leaf.path)) {
      patch = setAt(patch, leaf.path, null)
      continue
    }
    const next = getAt(edited, leaf.path)
    if (leaf.schema.type === 'secret' && isSecretView(next)) continue
    if (!deepEqual(getAt(original, leaf.path), next)) patch = setAt(patch, leaf.path, next === undefined ? null : next)
  }
  return patch
}

// A new, empty value for schema, for an added array item
export function emptyValue(schema: Schema): unknown {
  switch (schema.type) {
    case 'object':
      return Object.fromEntries(
        (schema.required ?? []).map((k) => [k, emptyValue(schema.properties?.[k] ?? { type: 'string' })]),
      )
    case 'array':
      return []
    case 'boolean':
      return false
    case 'enum':
      return schema.options?.[0]?.value ?? ''
    case 'number':
    case 'integer':
      return schema.min ?? 0
    default:
      return ''
  }
}

// The schema of the setting at path, array items by index
export function schemaAt(schema: Schema | undefined, path: string): Schema | undefined {
  let cur = schema
  for (const seg of segments(path)) cur = cur?.type === 'object' ? cur.properties?.[seg] : cur?.items
  return cur
}

// Whether schema's leaf at path takes values at scope
export function allowsScope(schema: Schema, path: string, sectionScopes: readonly Scope[], scope: Scope): boolean {
  let scopes = sectionScopes
  let cur: Schema | undefined = schema
  for (const seg of segments(path)) {
    cur = cur?.type === 'object' ? cur.properties?.[seg] : cur?.items
    if (cur?.scopes?.length) scopes = cur.scopes
  }
  return scopes.includes(scope)
}
