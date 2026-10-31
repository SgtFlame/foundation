// A markdown file in a volume, opened from the Volume editor and edited by
// everyone who has it open
export class MarkdownObject {
  readonly kind = 'markdown'
  readonly id: string
  readonly fileId: string // the collaboration room
  readonly path: readonly string[] // folders from the knowledge base root, then the file's name
  readonly type: string
  readonly name: string // tab text: the file name with its extension
  readonly helpText: string // tab hover: the full path, the root being "/"
  readonly readOnly: boolean // the user may read the Volume but not write it

  constructor(fileId: string, path: readonly string[], type: string, extension: string, readOnly = false) {
    this.id = `markdown-${fileId}`
    this.fileId = fileId
    this.path = path
    this.type = type
    this.name = `${path.at(-1)}${extension}`
    this.helpText = `/${path.join('/')}${extension}${readOnly ? ' (read-only)' : ''}`
    this.readOnly = readOnly
  }
}
