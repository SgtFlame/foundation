// The record form's merge (forge 04e › Record form): what this user typed
// is kept per field until it is saved, while the record keeps following
// everyone's changes. A field this user has not touched shows the record's
// value; a touched field another user changes meanwhile becomes a conflict,
// with their value and their name, which the user takes or overrides.

import { same, type RecordValues } from './fields.ts'

export interface Edit {
  value: unknown // what this user entered
  base: unknown // the record's value when they started, or the conflict they overrode
  // Another user's change to the field since base, and who made it
  conflict?: { value: unknown; by: string }
}

export type Edits = Record<string, Edit>

// The user entered a value: an edit back to where it started is dropped,
// unless it is the answer to a conflict
export function edit(edits: Edits, record: RecordValues | undefined, field: string, value: unknown): Edits {
  const old = edits[field]
  const base = old ? old.base : record?.[field]
  const next = { ...edits }
  if (same(value, base) && !old?.conflict) delete next[field]
  else next[field] = { ...old, value, base }
  return next
}

// The record changed from prev to next (a newer _seq): an edit the record
// now holds is done (this user's save, or the same change by someone
// else); one whose field another change moved becomes a conflict, named
// by who made that change (forge 04e question 3: the change's, not the
// record's, so a later change to another field does not rename it).
export function follow(edits: Edits, prev: RecordValues | undefined, next: RecordValues | undefined, nameOf: (userId: string) => string): Edits {
  if (!next) return edits
  let out: Edits | undefined
  for (const [field, e] of Object.entries(edits)) {
    const theirs = next[field]
    if (same(theirs, e.value)) {
      out ??= { ...edits }
      delete out[field]
      continue
    }
    if (prev && !same(theirs, prev[field]) && !same(theirs, e.base)) {
      out ??= { ...edits }
      out[field] = { ...e, conflict: { value: theirs, by: nameOf(String(next._updatedBy ?? '')) } }
    }
  }
  return out ?? edits
}

// Take the other user's value: the edit goes and the field shows the record
export function takeTheirs(edits: Edits, field: string): Edits {
  const next = { ...edits }
  delete next[field]
  return next
}

// Keep this user's value over the other's: it is based on theirs now
export function keepMine(edits: Edits, field: string): Edits {
  const e = edits[field]
  if (!e?.conflict) return edits
  return { ...edits, [field]: { value: e.value, base: e.conflict.value } }
}

// What a save sends: only the fields this user changed
export function changes(edits: Edits): RecordValues {
  return Object.fromEntries(Object.entries(edits).map(([f, e]) => [f, e.value ?? null]))
}

export function conflicts(edits: Edits): string[] {
  return Object.entries(edits).filter(([, e]) => e.conflict).map(([f]) => f)
}
