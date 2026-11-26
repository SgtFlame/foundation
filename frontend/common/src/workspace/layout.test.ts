// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test, mock } from 'node:test'
import assert from 'node:assert/strict'
import { layoutFromTabs, layoutToSave, readLayout, rowToShow, tabsOf, type LayoutNode, type SavedLayout } from './layout.ts'
import { LayoutSaver } from './layoutSaver.ts'

const tab = (id: string, component = id.split(':')[0]): LayoutNode => ({ type: 'tab', id, name: id, component, config: { object: { kind: component, id } } })
const restorable = (c: string) => c === 'prefab' || c.startsWith('app:')

// Two tabsets side by side: a prefab and a log query on the left, an app
// tab and a markdown file on the right
const split: LayoutNode = {
  type: 'row', weight: 100, children: [
    { type: 'tabset', id: 'left', weight: 60, selected: 1, children: [tab('logs:q'), tab('prefab:card')] },
    { type: 'tabset', id: 'right', weight: 40, active: true, selected: 1, children: [tab('app:tasks:main', 'app:tasks:main'), tab('markdown:m1')] },
    { type: 'tabset', id: 'only-logs', weight: 10, children: [tab('logs:other')] },
  ],
}

test('the saved layout keeps only restorable tabs and drops empty tabsets', () => {
  const saved = layoutToSave(split, restorable)
  assert.deepEqual(tabsOf(saved.layout).map((t) => t.id), ['prefab:card', 'app:tasks:main'])
  assert.deepEqual(saved.layout.children!.map((c) => c.id), ['left', 'right'])
  // the left tabset's selected prefab stays selected; the right one's
  // markdown is gone, so the tab now at its place (clamped) is
  assert.equal(saved.layout.children![0].selected, 0)
  assert.equal(saved.layout.children![1].selected, 0)
  assert.equal(saved.layout.children![1].active, true)
  assert.equal(saved.layout.children![0].weight, 60)
})

test('border tabs are never saved', () => {
  const saved = layoutToSave({ type: 'row', children: [{ type: 'tabset', children: [tab('border:chat', 'borderTab'), tab('prefab:a')] }] }, () => true)
  assert.deepEqual(tabsOf(saved.layout).map((t) => t.id), ['prefab:a'])
})

test('a split layout restores as it was', () => {
  const saved = JSON.parse(JSON.stringify(layoutToSave(split, restorable))) as unknown
  const read = readLayout(saved)
  assert.ok(read)
  const row = rowToShow(read.layout)
  assert.deepEqual(row, read.layout)
  assert.deepEqual(row.children!.map((c) => [c.id, c.weight, tabsOf(c).map((t) => t.id)]), [
    ['left', 60, ['prefab:card']],
    ['right', 40, ['app:tasks:main']],
  ])
})

test('a layout this code cannot read is none', () => {
  assert.equal(readLayout(undefined), undefined)
  assert.equal(readLayout({ tabs: [] }), undefined)
  assert.equal(readLayout({ format: 2, layout: { type: 'row' } }), undefined)
})

test('kept tabs join the active tabset, and an empty row gets a tabset', () => {
  const row = rowToShow(layoutToSave(split, restorable).layout, [tab('logs:q'), tab('prefab:card')])
  assert.deepEqual(tabsOf(row.children![1]).map((t) => t.id), ['app:tasks:main', 'logs:q'])
  const empty = rowToShow({ type: 'row', children: [] }, [tab('logs:q')])
  assert.equal(empty.children!.length, 1)
  assert.deepEqual(tabsOf(empty).map((t) => t.id), ['logs:q'])
  assert.deepEqual(tabsOf(rowToShow(undefined)), [])
})

test('the tabs saved before workspaces move in as one tabset', () => {
  const saved = layoutFromTabs({ tabs: [{ id: 'prefab:a', name: 'A', component: 'prefab', config: {} }, { id: 'app:x:main', name: 'X', component: 'app:x:main', config: {} }], selected: 'app:x:main' })
  assert.equal(saved.layout.children!.length, 1)
  assert.equal(saved.layout.children![0].selected, 1)
  assert.deepEqual(tabsOf(saved.layout).map((t) => t.id), ['prefab:a', 'app:x:main'])
})

// A server holding one workspace, for the saver
function fakeServer() {
  const ws = { version: 3, layout: undefined as unknown }
  const sends: { version: number; layout: SavedLayout }[] = []
  let failNext = false
  return {
    ws, sends,
    staleOnce: () => { failNext = true },
    send: async (version: number, layout: SavedLayout) => {
      sends.push({ version, layout })
      if (failNext || version !== ws.version) {
        failNext = false
        throw Object.assign(new Error('stale'), { errorCode: 'stale_workspace' })
      }
      ws.version++
      ws.layout = JSON.parse(JSON.stringify(layout))
    },
  }
}
const layoutOf = (n: number): SavedLayout => ({ format: 1, layout: { type: 'row', children: [{ type: 'tabset', weight: n, children: [] }] } })
const tick = () => new Promise((r) => setImmediate(r))

test('a drag sends one update after the changes stop', async () => {
  mock.timers.enable({ apis: ['setTimeout'] })
  try {
    const srv = fakeServer()
    let weight = 0
    const saver = new LayoutSaver({ layout: () => layoutOf(weight), seen: () => srv.ws, send: srv.send }, 500)
    for (let i = 1; i <= 20; i++) {
      weight = i
      saver.schedule()
      mock.timers.tick(50)
    }
    assert.equal(srv.sends.length, 0)
    mock.timers.tick(500)
    await tick()
    assert.equal(srv.sends.length, 1)
    assert.equal(srv.sends[0].layout.layout.children![0].weight, 20)
    assert.equal(saver.acked, 4)
    // the same layout again sends nothing
    saver.schedule()
    mock.timers.tick(500)
    await tick()
    assert.equal(srv.sends.length, 1)
  } finally {
    mock.timers.reset()
  }
})

test('a stale save is sent again on the newer version', async () => {
  mock.timers.enable({ apis: ['setTimeout'] })
  try {
    const srv = fakeServer()
    // the browser has seen version 3, but another window saved version 4
    const seen = { version: 3, layout: undefined as unknown }
    srv.ws.version = 4
    const saver = new LayoutSaver({ layout: () => layoutOf(7), seen: () => seen, send: srv.send }, 500)
    saver.schedule()
    mock.timers.tick(500)
    await tick()
    assert.equal(srv.sends.length, 1)
    assert.equal(saver.idle(), false)
    seen.version = 4 // the other window's change arrives
    saver.versionSeen()
    await tick()
    assert.equal(srv.sends.length, 2)
    assert.equal(srv.sends[1].version, 4)
    assert.equal(srv.ws.version, 5)
    assert.equal(saver.idle(), true)
  } finally {
    mock.timers.reset()
  }
})

test('a retired saver sends the layout it had, not the next workspace\'s', async () => {
  mock.timers.enable({ apis: ['setTimeout'] })
  try {
    const srv = fakeServer()
    let weight = 1
    const saver = new LayoutSaver({ layout: () => layoutOf(weight), seen: () => srv.ws, send: srv.send }, 500)
    saver.schedule()
    saver.retire()
    weight = 99 // the model now shows another workspace
    await tick()
    assert.equal(srv.sends.length, 1)
    assert.equal(srv.sends[0].layout.layout.children![0].weight, 1)
    saver.schedule()
    mock.timers.tick(500)
    await tick()
    assert.equal(srv.sends.length, 1)
  } finally {
    mock.timers.reset()
  }
})

test('a window that must not save sends nothing', async () => {
  mock.timers.enable({ apis: ['setTimeout'] })
  try {
    const srv = fakeServer()
    const saver = new LayoutSaver({ layout: () => undefined, seen: () => srv.ws, send: srv.send }, 500)
    saver.schedule()
    mock.timers.tick(500)
    await tick()
    assert.equal(srv.sends.length, 0)
  } finally {
    mock.timers.reset()
  }
})
