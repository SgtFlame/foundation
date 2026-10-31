import type { EditableObject } from '../../workbench/points.ts'

// A Cognition graph in the Cognition Editor: id `graph-<graphId>`
export class CognitionObject implements EditableObject {
  readonly kind = 'cognition'
  readonly id: string
  readonly graphId: string
  readonly name: string
  readonly helpText?: string

  constructor(graph: { id: string; name: string; description?: string }) {
    this.graphId = graph.id
    this.id = `graph-${graph.id}`
    this.name = graph.name
    this.helpText = graph.description
  }
}
