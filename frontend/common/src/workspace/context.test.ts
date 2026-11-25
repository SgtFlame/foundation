// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { ContextWriter, differing, isChecked, parseContext, withCheck, type WorkspaceContext } from './context.ts'

const flush = () => new Promise((r) => setTimeout(r, 0))

// A workspace on the server: workspace.update with its version check
function server(context: WorkspaceContext = {}) {
  const ws = { version: 1, context: context as unknown }
  const seen = { version: 1, context: context as unknown } // what this browser's list has
  const sends: { version: number; context: WorkspaceContext }[] = []
  const send = async (version: number, ctx: WorkspaceContext) => {
    sends.push({ version, context: ctx })
    if (version !== ws.version) throw Object.assign(new Error('stale'), { errorCode: 'stale_workspace' })
    ws.version++
    ws.context = ctx
  }
  // the list brings the server's version
  const arrive = () => {
    seen.version = ws.version
    seen.context = ws.context
  }
  return { ws, seen, sends, send, arrive }
}

test('Volumes are checked and entries unchecked unless the workspace says otherwise', () => {
  const ctx = parseContext({ volume: { '41': false, '42': true }, chronicle: { '7': true }, bad: [1], channel: { c: 'yes' } })
  assert.equal(isChecked(ctx, 'volume', '41'), false)
  assert.equal(isChecked(ctx, 'volume', '99'), true)
  assert.equal(isChecked(ctx, 'chronicle', '7'), true)
  assert.equal(isChecked(ctx, 'chronicle', '8'), false)
  assert.deepEqual(differing(ctx, 'volume'), ['41'])
  assert.deepEqual(parseContext('nope'), {})
})

test('only differences from the defaults are stored', () => {
  let ctx = withCheck({}, 'volume', '41', false)
  ctx = withCheck(ctx, 'chronicle', '7', true)
  assert.deepEqual(ctx, { volume: { '41': false }, chronicle: { '7': true } })
  ctx = withCheck(withCheck(ctx, 'volume', '41', true), 'chronicle', '7', false)
  assert.deepEqual(ctx, {})
})

test('a toggle shows at once, is written once, and stays until the list has it', async () => {
  const s = server()
  const w = new ContextWriter({ seen: () => s.seen, send: s.send })
  w.setChecked('volume', '41', false)
  assert.equal(isChecked(w.effective(), 'volume', '41'), false)
  await flush()
  assert.deepEqual(s.sends, [{ version: 1, context: { volume: { '41': false } } }])
  assert.equal(isChecked(w.effective(), 'volume', '41'), false) // acknowledged, not seen yet
  s.arrive()
  w.versionSeen()
  assert.ok(w.idle())
  assert.deepEqual(w.effective(), { volume: { '41': false } })
})

test('toggles made while a write is out go next, on the version it made', async () => {
  const s = server()
  const w = new ContextWriter({ seen: () => s.seen, send: s.send })
  w.setChecked('volume', '41', false)
  w.setChecked('chronicle', '7', true)
  await flush()
  await flush()
  assert.equal(s.sends.length, 2)
  assert.equal(s.sends[1].version, 2)
  assert.deepEqual(s.ws.context, { volume: { '41': false }, chronicle: { '7': true } })
})

test("a stale write is re-applied on the newer version, keeping the other window's toggle", async () => {
  const s = server()
  const w = new ContextWriter({ seen: () => s.seen, send: s.send })
  // another window checked entry 7 first
  await s.send(1, { chronicle: { '7': true } })
  w.setChecked('volume', '41', false)
  await flush()
  assert.equal(s.sends.at(-1)!.version, 1) // refused
  s.arrive()
  w.versionSeen()
  await flush()
  assert.deepEqual(s.ws.context, { chronicle: { '7': true }, volume: { '41': false } })
})

test('a toggle undone while it is being written is written back', async () => {
  const s = server()
  const w = new ContextWriter({ seen: () => s.seen, send: s.send })
  w.setChecked('volume', '41', false) // goes out at once
  w.setChecked('volume', '41', true)
  assert.equal(isChecked(w.effective(), 'volume', '41'), true)
  await flush()
  await flush()
  assert.deepEqual(s.sends.map((x) => x.version), [1, 2])
  assert.deepEqual(s.ws.context, {})
  s.arrive()
  w.versionSeen()
  assert.ok(w.idle())
})

test('toggling twice before anything is sent sends nothing', async () => {
  const s = server({ volume: { '41': false } })
  const w = new ContextWriter({ seen: () => s.seen, send: s.send })
  w.setChecked('volume', '41', false) // as it is already
  await flush()
  assert.equal(s.sends.length, 0)
  assert.ok(w.idle())
})

test('a failed write shows the saved checks again', async () => {
  let failed: unknown
  const w = new ContextWriter({
    seen: () => ({ version: 1, context: {} }),
    send: async () => {
      throw new Error('offline')
    },
    failed: (err) => (failed = err),
  })
  w.setChecked('volume', '41', false)
  await flush()
  assert.ok(failed)
  assert.equal(isChecked(w.effective(), 'volume', '41'), true)
})
