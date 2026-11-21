// A Knowledge Base Volume, opened from the Volumes editor
export class VolumeObject {
  readonly kind = 'volume'
  readonly id: string
  readonly tsid: string
  readonly path: readonly string[] // folders from the knowledge base root
  readonly name: string // tab text: the volume's folder name
  readonly helpText: string // tab hover: the full path, the root being "/"

  constructor(tsid: string, path: readonly string[]) {
    this.id = `volume-${tsid}`
    this.tsid = tsid
    this.path = path
    this.name = path.at(-1) ?? '/'
    this.helpText = `/${path.join('/')}`
  }
}
