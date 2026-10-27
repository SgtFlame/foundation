// Find/replace edits, applied as the server's collabservice.ApplyEdits
// applies them: in order, each find occurring exactly once in the text the
// edits before it left. The kb.edit tool sends them to the room's writer.

export interface TextEdit {
  find: string
  replace: string
}

export interface PlacedEdit {
  index: number // in the text the edits before it left
  length: number
  replace: string
}

// Places every edit, or names the first one that cannot be placed
export function placeEdits(text: string, edits: readonly TextEdit[]): PlacedEdit[] | string {
  const placed: PlacedEdit[] = []
  for (const [i, e] of edits.entries()) {
    if (!e.find) return `edit ${i + 1} has nothing to find`
    const index = text.indexOf(e.find)
    if (index < 0) return `edit ${i + 1}: the text to find is not in the file`
    const count = text.split(e.find).length - 1
    if (count > 1) return `edit ${i + 1}: the text to find occurs ${count} times; include more of the text around it`
    placed.push({ index, length: e.find.length, replace: e.replace })
    text = text.slice(0, index) + e.replace + text.slice(index + e.find.length)
  }
  return placed
}
