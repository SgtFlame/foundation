// Knowledge Base Volumes: the root directories of the knowledge base
export class VolumesObject {
  readonly kind = 'volumes'
  readonly id: string

  constructor(id: string) {
    this.id = id
  }
}
