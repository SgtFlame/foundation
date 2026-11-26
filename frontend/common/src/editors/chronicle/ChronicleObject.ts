import type { EditableObject } from '../../workbench/points.ts'

let drafts = 0

// A Chronicle entry in the entry editor: id `entry-<id>`, or `new-<n>` for an
// entry not yet created, which may start with a type and a parent
export class ChronicleObject implements EditableObject {
  readonly kind = 'chronicle'
  readonly id: string
  readonly entryId?: string
  readonly name?: string
  readonly helpText?: string
  readonly draft?: { type?: string; parentId?: string }

  constructor(entry: { id?: string; title?: string; type?: string; parentId?: string } = {}) {
    this.entryId = entry.id
    this.id = entry.id ? `entry-${entry.id}` : `new-${++drafts}`
    this.name = entry.id ? entry.title : 'New Chronicle'
    this.helpText = entry.id ? `Chronicle entry ${entry.title ?? ''}`.trim() : undefined
    if (!entry.id) this.draft = { type: entry.type, parentId: entry.parentId }
  }
}
