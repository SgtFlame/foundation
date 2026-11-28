// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { complete, mentionsIn, suggestEmoji, suggestMentions, tokenAt, typingDue, type Mentionable } from './composerText.ts'

const people: Mentionable[] = [
  { id: '7', name: 'ana', label: 'Ana Lima', kind: 'user' },
  { id: '8', name: 'bo', label: 'Bo Chen', kind: 'user' },
  { id: 'agent:42', name: 'support', label: 'Support agent', kind: 'agent' },
  { id: '9', name: 'anders', label: 'Anders Berg', kind: 'user' },
]

test('the token the caret ends is found, and only after a space', () => {
  assert.deepEqual(tokenAt('hi @an', 6), { kind: 'mention', query: 'an', start: 3, end: 6 })
  assert.deepEqual(tokenAt('@', 1), { kind: 'mention', query: '', start: 0, end: 1 })
  assert.equal(tokenAt('mail@an', 7), undefined) // an address, not a mention
  assert.deepEqual(tokenAt('nice :thu', 9), { kind: 'emoji', query: 'thu', start: 5, end: 9 })
  assert.equal(tokenAt('ok :)', 5), undefined)
  assert.equal(tokenAt('ok :t', 5), undefined) // too short to search
  assert.equal(tokenAt('@ana done', 9), undefined)
})

test('mentions are suggested by name, then by the shown name', () => {
  assert.deepEqual(suggestMentions(people, 'an').map((p) => p.id), ['7', '9'])
  assert.deepEqual(suggestMentions(people, 'sup').map((p) => p.id), ['agent:42'])
  assert.deepEqual(suggestMentions(people, 'chen').map((p) => p.id), ['8'])
  assert.equal(suggestMentions(people, '').length, 4)
})

test('emoji are suggested by name', () => {
  assert.equal(suggestEmoji('thumbs')[0].char, '👍')
  assert.ok(suggestEmoji('rock').some((e) => e.char === '🚀'))
})

test('completing replaces the token and puts the caret after a space', () => {
  const t = tokenAt('hi @an and', 6)!
  assert.deepEqual(complete('hi @an and', t, '@ana'), { text: 'hi @ana and', caret: 8 })
  const e = tokenAt('yes :thu', 8)!
  assert.deepEqual(complete('yes :thu', e, '👍'), { text: 'yes 👍 ', caret: 7 })
})

test('a sent text names the people it mentions once, in order', () => {
  assert.deepEqual(mentionsIn('@bo can you and @ana look? thanks @bo.', people), ['8', '7'])
  assert.deepEqual(mentionsIn('ask @support, not @nobody', people), ['agent:42'])
  assert.deepEqual(mentionsIn('mail ana@example.com', people), [])
})

test('typing is signalled at most once per interval', () => {
  assert.equal(typingDue(0, 5000), true)
  assert.equal(typingDue(5000, 6000), false)
  assert.equal(typingDue(5000, 8000), true)
})
