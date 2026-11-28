// A document file in a Volume (forge 35b-1), opened from the Volume editor
// or a citation and edited by everyone who has it open
export class RichTextObject {
  readonly kind = 'richtext'
  readonly id: string
  readonly fileId: string // the collaboration room
  readonly path: readonly string[] // folders from the knowledge base root, then the file's name
  readonly name: string // tab text: the file name with its extension
  readonly helpText: string // tab hover: the full path, the root being "/"
  readonly readOnly: boolean // the user may read the Volume but not write it

  constructor(fileId: string, path: readonly string[], extension: string, readOnly = false) {
    this.id = `richtext-${fileId}`
    this.fileId = fileId
    this.path = path
    this.name = `${path.at(-1)}${extension}`
    this.helpText = `/${path.join('/')}${extension}${readOnly ? ' (read-only)' : ''}`
    this.readOnly = readOnly
  }
}
