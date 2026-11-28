// chat.composer's text logic (forge 26d-2), kept free of React so it is
// tested with node --test: which token the caret is in, the suggestions
// for it, completing it, and the mentions a sent text holds.

export interface Mentionable {
  id: string // a user id, or agent:<id>
  name: string // what follows @, without spaces: ana, support-agent
  label?: string // the shown name: Ana Lima
  kind?: string // user, agent
}

export interface Emoji {
  name: string
  char: string
}

// A small set of everyday emoji, by name, for :name completion.
export const EMOJI: Emoji[] = [
  ['smile', '😄'], ['grin', '😁'], ['joy', '😂'], ['wink', '😉'], ['blush', '😊'], ['heart_eyes', '😍'], ['thinking', '🤔'],
  ['neutral', '😐'], ['sweat', '😅'], ['cry', '😢'], ['sob', '😭'], ['angry', '😠'], ['scream', '😱'], ['sunglasses', '😎'],
  ['thumbsup', '👍'], ['thumbsdown', '👎'], ['clap', '👏'], ['wave', '👋'], ['pray', '🙏'], ['muscle', '💪'], ['ok_hand', '👌'],
  ['raised_hands', '🙌'], ['eyes', '👀'], ['heart', '❤️'], ['fire', '🔥'], ['tada', '🎉'], ['rocket', '🚀'], ['star', '⭐'],
  ['sparkles', '✨'], ['100', '💯'], ['check', '✅'], ['x', '❌'], ['warning', '⚠️'], ['question', '❓'], ['bulb', '💡'],
  ['memo', '📝'], ['calendar', '📅'], ['bug', '🐛'], ['coffee', '☕'], ['pizza', '🍕'], ['party', '🥳'], ['plus1', '➕'],
].map(([name, char]) => ({ name, char }))

// Token is the word the caret ends: @ana or :smi, with where it starts.
export interface Token {
  kind: 'mention' | 'emoji'
  query: string
  start: number // index of @ or :
  end: number // the caret
}

// tokenAt finds the mention or emoji token the caret is at the end of: @
// or : at the start of the text or after a space, then word characters.
export function tokenAt(text: string, caret: number): Token | undefined {
  const before = text.slice(0, caret)
  const m = /(^|\s)([@:])([\w.-]*)$/.exec(before)
  if (!m) return undefined
  const start = caret - m[3].length - 1
  if (m[2] === ':' && m[3].length < 2) return undefined // ":" alone, or ":)", is not a search
  return { kind: m[2] === '@' ? 'mention' : 'emoji', query: m[3], start, end: caret }
}

// suggest lists what completes a token, best first, at most limit.
export function suggestMentions(people: readonly Mentionable[], query: string, limit = 8): Mentionable[] {
  const q = query.toLowerCase()
  const scored = people
    .map((p) => {
      const name = p.name.toLowerCase()
      const label = (p.label ?? '').toLowerCase()
      const score = name.startsWith(q) ? 0 : label.startsWith(q) ? 1 : name.includes(q) || label.includes(q) ? 2 : -1
      return { p, score }
    })
    .filter((s) => s.score >= 0)
  scored.sort((a, b) => a.score - b.score || a.p.name.localeCompare(b.p.name))
  return scored.slice(0, limit).map((s) => s.p)
}

export function suggestEmoji(query: string, limit = 8, set: readonly Emoji[] = EMOJI): Emoji[] {
  const q = query.toLowerCase()
  const starts = set.filter((e) => e.name.startsWith(q))
  const has = set.filter((e) => !e.name.startsWith(q) && e.name.includes(q))
  return [...starts, ...has].slice(0, limit)
}

// complete replaces the token with what was chosen, followed by a space,
// and gives the new text and caret.
export function complete(text: string, token: Token, insert: string): { text: string; caret: number } {
  const next = text.slice(0, token.start) + insert + ' ' + text.slice(token.end).replace(/^ /, '')
  return { text: next, caret: token.start + insert.length + 1 }
}

// mentionsIn are the ids of the people a text mentions by @name, once
// each, in order; a name nobody has is plain text.
export function mentionsIn(text: string, people: readonly Mentionable[]): string[] {
  const byName = new Map(people.map((p) => [p.name.toLowerCase(), p.id]))
  const out: string[] = []
  for (const m of text.matchAll(/(^|\s)@([\w.-]+)/g)) {
    const name = m[2].replace(/[.-]+$/, '').toLowerCase()
    const id = byName.get(name)
    if (id && !out.includes(id)) out.push(id)
  }
  return out
}

// Throttle says whether a typing signal is due: at most one per interval
// while typing goes on.
export function typingDue(last: number, now: number, interval = 3000): boolean {
  return now - last >= interval
}
