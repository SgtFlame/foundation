// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { chooseCurrent, copyName, matchWorkspace, nameProblem, type Workspace } from './current.ts'

const ws = (id: string, name: string, extra: Partial<Workspace> = {}): Workspace => ({ id, name, position: 0, version: 1, ...extra })
const def = ws('default', 'Default', { isDefault: true })
const support = ws('w1', 'Support')
const sales = ws('w2', 'Sales')
const list = [def, support, sales]

test('a tab keeps its own workspace while it exists', () => {
  assert.equal(chooseCurrent(list, 'w1', 'w2'), 'w1')
})

test('a new tab opens the last one used in this browser', () => {
  assert.equal(chooseCurrent(list, null, 'w2'), 'w2')
  assert.equal(chooseCurrent(list, undefined, undefined), 'default')
})

test('a workspace deleted elsewhere falls back to Default, not to the last one', () => {
  assert.equal(chooseCurrent([def, sales], 'w1', 'w2'), 'default')
  assert.equal(chooseCurrent([def, sales], null, 'w1'), 'default')
})

test('nothing is current before Default is made', () => {
  assert.equal(chooseCurrent([], 'w1', 'w2'), undefined)
})

test('names are unique per person, ignoring case and punctuation', () => {
  assert.equal(nameProblem('support!', list), 'You already have a workspace called Support')
  assert.equal(nameProblem('Support', list, 'w1'), undefined)
  assert.equal(nameProblem('  ', list), 'A name needs letters or digits')
  assert.equal(nameProblem('x'.repeat(61), list), 'At most 60 characters')
  assert.equal(nameProblem('Billing', list), undefined)
})

test('a duplicate gets the first free copy name', () => {
  assert.equal(copyName('Support', list), 'Support copy')
  assert.equal(copyName('Support', [...list, ws('w3', 'Support copy')]), 'Support copy 2')
})

test('/workspace matches a name or a unique prefix', () => {
  assert.deepEqual(matchWorkspace('SUPPORT', list), { kind: 'found', workspace: support })
  assert.deepEqual(matchWorkspace('sal', list), { kind: 'found', workspace: sales })
  assert.equal(matchWorkspace('s', list).kind, 'ambiguous')
  assert.equal(matchWorkspace('billing', list).kind, 'unknown')
  // an exact name wins over a longer one it prefixes
  assert.deepEqual(matchWorkspace('sales', [...list, ws('w4', 'Sales leads')]), { kind: 'found', workspace: sales })
})
