import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { fit, type PieceSet } from '../area/PieceSet.ts'
import { farmstead, hawksRest, SIGNPOST } from '../area/Samples.ts'
import { dragMarks, drawCard, LESSER_DECK, nextStep, STORY_STEPS, storySteps } from './FirstHourStory.ts'

test("the first hour's steps are the server's (S6)", () => {
  const go = readFileSync(new URL('../../../../backend/features/game/Story.go', import.meta.url), 'utf8')
  const ids = /StorySteps = \[\]string\{([^}]*)\}/.exec(go)![1].match(/"([^"]+)"/g)!.map((s) => s.slice(1, -1))
  assert.deepEqual(
    STORY_STEPS.map((s) => s.id),
    ids,
  )
  // the reachStory graph accepts each
  const graph = readFileSync(new URL('../../../../backend/features/game/graphs/cmd-game-reachStory.json', import.meta.url), 'utf8')
  for (const id of ids) assert.ok(graph.includes(`\\"${id}\\"`), id)
})

test('a later step done counts the earlier ones, and the next is the first left', () => {
  assert.equal(nextStep({}), 'arrive')
  assert.equal(nextStep({ arrive: true, farmer: true }), 'howl')
  const s = storySteps({ fight: true })
  assert.ok(s.slice(0, 6).every((x) => x.done))
  assert.equal(nextStep({ fight: true }), 'road')
  assert.equal(nextStep({ rest: true }), null)
})

test('drag marks run from the signature toward the barn, and the same seed draws the same card', () => {
  const m = dragMarks({ x: 50, z: 40 }, { x: 20, z: 40 })
  assert.ok(m.length >= 15)
  assert.ok(m[0].x < 50 && m[m.length - 1].x > 20)
  assert.ok(m.every((p) => Math.abs(p.z - 40) < 0.5))
  assert.equal(drawCard(7).id, drawCard(7).id)
  assert.ok(new Set(Array.from({ length: 40 }, (_, i) => drawCard(i).id)).size === LESSER_DECK.length)
})

test("the signpost fits the farmstead where the road leaves, and Hawk's Rest fits the kitchen set", () => {
  const farmland = JSON.parse(readFileSync(new URL('../../../../backend/features/game/piecesets/farmland.json', import.meta.url), 'utf8')) as PieceSet
  const pieces = Object.fromEntries(farmstead.pieces.map((p, i) => [`p${i}`, { ...p, id: `p${i}` }]))
  assert.equal(fit(farmland, farmstead.width, farmstead.depth, pieces, { id: '', ...SIGNPOST }), '')
  const kitchen = JSON.parse(readFileSync(new URL('../../../../backend/features/game/piecesets/kitchen.json', import.meta.url), 'utf8')) as PieceSet
  const placed: Record<string, (typeof hawksRest.pieces)[number] & { id: string }> = {}
  for (const [i, p] of hawksRest.pieces.entries()) {
    const id = p.id ?? `h${i}`
    assert.equal(fit(kitchen, hawksRest.width, hawksRest.depth, placed, { ...p, id }), '', p.piece)
    placed[id] = { ...p, id }
  }
})
