// The paragraph styles of the rich-text editor (forge 35b-1): Normal and
// Heading 1 to 4 as named styles, each a node (paragraph or heading with
// a level) and how it looks. A tenant's or workspace's own (the docs.styles
// setting) override them by id; until that setting is built, overrides are
// passed in. Pure, so it is tested on its own.

export interface ParagraphStyle {
  id: string
  name: string
  node: 'paragraph' | 'heading'
  level?: number
  fontSize: string // CSS, in em of the editor's base size
  fontWeight: number
  marginTop: string
}

export type StyleOverride = Partial<Omit<ParagraphStyle, 'id' | 'node' | 'level'>> & { id: string }

export const defaultStyles: readonly ParagraphStyle[] = [
  { id: 'normal', name: 'Normal text', node: 'paragraph', fontSize: '1em', fontWeight: 400, marginTop: '0.5em' },
  { id: 'heading1', name: 'Heading 1', node: 'heading', level: 1, fontSize: '1.9em', fontWeight: 600, marginTop: '1.2em' },
  { id: 'heading2', name: 'Heading 2', node: 'heading', level: 2, fontSize: '1.5em', fontWeight: 600, marginTop: '1.1em' },
  { id: 'heading3', name: 'Heading 3', node: 'heading', level: 3, fontSize: '1.25em', fontWeight: 600, marginTop: '1em' },
  { id: 'heading4', name: 'Heading 4', node: 'heading', level: 4, fontSize: '1.1em', fontWeight: 600, marginTop: '0.9em' },
]

// The styles with overrides applied by id; an override naming no style is
// ignored, and node and level never change
export function paragraphStyles(overrides: readonly StyleOverride[] = []): ParagraphStyle[] {
  const byId = new Map(overrides.map((o) => [o.id, o]))
  return defaultStyles.map((s) => {
    const o = byId.get(s.id)
    return o ? { ...s, ...o, id: s.id, node: s.node, level: s.level } : { ...s }
  })
}

// The style a block has: its heading level's, else Normal for a paragraph;
// undefined for any other block (a list item's paragraph is Normal)
export function styleOf(styles: readonly ParagraphStyle[], node: string, level?: number): ParagraphStyle | undefined {
  if (node === 'heading') return styles.find((s) => s.node === 'heading' && s.level === level)
  if (node === 'paragraph') return styles.find((s) => s.node === 'paragraph')
  return undefined
}

// CSS for the editor's content: one rule per style
export function stylesCss(styles: readonly ParagraphStyle[]): Record<string, Record<string, string | number>> {
  const out: Record<string, Record<string, string | number>> = {}
  for (const s of styles) {
    const selector = s.node === 'heading' ? `& h${s.level}` : '& p'
    out[selector] = { fontSize: s.fontSize, fontWeight: s.fontWeight, marginTop: s.marginTop, marginBottom: 0 }
  }
  return out
}
