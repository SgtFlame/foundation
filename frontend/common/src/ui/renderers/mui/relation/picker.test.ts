// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test, mock } from 'node:test'
import assert from 'node:assert/strict'
import { debounce, idsOf, matchesWhere, offerCreate, pick, queryFor, remove, valueOf } from './picker.ts'

test('values as ids and back', () => {
  assert.deepEqual(idsOf(null), [])
  assert.deepEqual(idsOf(''), [])
  assert.deepEqual(idsOf('7'), ['7'])
  assert.deepEqual(idsOf(['7', '8', '7']), ['7', '8'])
  assert.deepEqual(idsOf([{ _id: '9' }, { id: '10' }]), ['9', '10'])
  assert.equal(valueOf([], false), null)
  assert.equal(valueOf(['1', '2'], false), '1')
  assert.deepEqual(valueOf([], true), [])
})

test('single replaces, multiple adds once, remove drops', () => {
  assert.deepEqual(pick(['1'], '2', false), ['2'])
  assert.deepEqual(pick(['1'], '2', true), ['1', '2'])
  assert.deepEqual(pick(['1', '2'], '1', true), ['1', '2'])
  assert.deepEqual(pick(['1'], '', true), ['1'])
  assert.deepEqual(remove(['1', '2'], '1'), ['2'])
})

test('Create is offered only for new text', () => {
  assert.equal(offerCreate(true, '  Acme ', ['Globex']), 'Acme')
  assert.equal(offerCreate(true, 'acme', ['Acme ']), null)
  assert.equal(offerCreate(false, 'Acme', []), null)
  assert.equal(offerCreate(true, '   ', []), null)
})

test('search results narrowed by where', () => {
  assert.ok(matchesWhere({ company: 'c1' }, { company: 'c1' }))
  assert.ok(matchesWhere({ company: { _id: 'c1' } }, { company: 'c1' }))
  assert.ok(!matchesWhere({ company: 'c2' }, { company: 'c1' }))
  assert.ok(matchesWhere({ tags: ['a', 'b'] }, { tags: 'a' }))
  assert.ok(matchesWhere({ x: 1 }, undefined))
})

test('a keystroke searches or scans the title prefix', () => {
  assert.deepEqual(queryFor('contact', 'ad', { searchable: true, titleField: 'name' }), { kind: 'search', request: { entity: 'contact', text: 'ad', limit: 40 } })
  assert.deepEqual(queryFor('contact', 'a', { searchable: true, titleField: 'name', where: { company: 'c1' } }), {
    kind: 'find',
    request: { entity: 'contact', limit: 20, where: { company: 'c1' }, sort: [{ field: 'name' }], filter: { field: 'name', op: 'prefix', value: 'a' } },
  })
  assert.deepEqual(queryFor('contact', '', { searchable: false, titleField: 'name' }), { kind: 'find', request: { entity: 'contact', limit: 20, sort: [{ field: 'name' }] } })
})

test('debounce sends only the last keystroke after the pause', () => {
  mock.timers.enable({ apis: ['setTimeout'] })
  try {
    const seen: string[] = []
    const d = debounce((t: string) => seen.push(t), 250)
    d.call('a')
    mock.timers.tick(100)
    d.call('ab')
    mock.timers.tick(249)
    assert.deepEqual(seen, [])
    mock.timers.tick(1)
    assert.deepEqual(seen, ['ab'])
    d.call('abc')
    d.cancel()
    mock.timers.tick(500)
    assert.deepEqual(seen, ['ab'])
  } finally {
    mock.timers.reset()
  }
})
