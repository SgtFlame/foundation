import { test } from 'node:test'
import assert from 'node:assert/strict'
import { defaultStyles, paragraphStyles, styleOf, stylesCss } from './styles.ts'

test('overrides apply by id and keep the node', () => {
  const styles = paragraphStyles([{ id: 'heading1', name: 'Chapter', fontSize: '2.4em' }, { id: 'nope', name: 'Ignored' }])
  const h1 = styles.find((s) => s.id === 'heading1')
  assert.equal(h1?.name, 'Chapter')
  assert.equal(h1?.fontSize, '2.4em')
  assert.equal(h1?.node, 'heading')
  assert.equal(h1?.level, 1)
  assert.equal(styles.length, defaultStyles.length)
  assert.equal(defaultStyles[1].name, 'Heading 1', 'the defaults are not changed')
})

test('a block has its style', () => {
  const styles = paragraphStyles()
  assert.equal(styleOf(styles, 'heading', 2)?.id, 'heading2')
  assert.equal(styleOf(styles, 'paragraph')?.id, 'normal')
  assert.equal(styleOf(styles, 'heading', 6), undefined)
  assert.equal(styleOf(styles, 'codeBlock'), undefined)
})

test('one CSS rule per style', () => {
  const css = stylesCss(paragraphStyles())
  assert.deepEqual(Object.keys(css), ['& p', '& h1', '& h2', '& h3', '& h4'])
  assert.equal(css['& h1'].fontWeight, 600)
})
