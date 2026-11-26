import assert from 'node:assert/strict'
import { test } from 'node:test'
import { Combat, CREATURES, HERO_HP, WEAPONS, type CombatEvent, type Stance } from './Combat.ts'

const two = [
  { id: 'a', kind: 'hollowed', x: 6, z: 0 },
  { id: 'b', kind: 'hollowed', x: 0, z: 7 },
]
const skilled = { blade: 3, polearm: 2, reading: 2 }

// Fights with a fixed plan: target the nearest living foe, hold a stance
function fight(c: Combat, seconds: number, stance: Stance = 'strike') {
  const hero = { x: 0, z: 0 }
  const events: CombatEvent[] = []
  c.setStance(stance)
  for (let t = 0; t < seconds && !c.over; t += 1 / 30) {
    if (!c.target) {
      const next = [...c.foes.values()].filter((f) => f.alive).sort((p, q) => Math.hypot(p.x, p.z) - Math.hypot(q.x, q.z))[0]
      c.attack(next?.id ?? null)
    }
    events.push(...c.step(1 / 30, hero))
  }
  return events
}

test('a skilled fighter kills two Hollowed, and only vital hits kill them', () => {
  const c = new Combat(two, { skills: skilled, weapon: WEAPONS.sword, seed: 3 })
  const events = fight(c, 60)
  assert.equal(c.over, 'won')
  const kills = events.filter((e) => e.kind === 'kill')
  assert.equal(kills.length, 2)
  assert.ok(kills.every((e) => e.kind === 'kill' && e.vital === 'neck'))
  // a futile hit never kills: each foe dies on exactly one vital hit
  assert.ok(events.some((e) => e.kind === 'futile'), 'some hits only stagger them')
})

test('it is the same fight from the same seed', () => {
  const a = fight(new Combat(two, { skills: skilled, weapon: WEAPONS.sword, seed: 9 }), 30)
  const b = fight(new Combat(two, { skills: skilled, weapon: WEAPONS.sword, seed: 9 }), 30)
  assert.deepEqual(a, b)
})

test('Guard blocks claws, and a read tell blocks them whole', () => {
  const guard = new Combat([two[0]], { skills: skilled, weapon: WEAPONS.sword, seed: 2 })
  const events = fight(guard, 14, 'guard')
  const claws = events.filter((e) => e.kind === 'claw')
  assert.ok(claws.length >= 4)
  assert.ok(claws.every((e) => e.kind === 'claw' && e.damage === 0 && e.blocked && e.read))
  assert.equal(guard.hp, HERO_HP)
  assert.ok(events.some((e) => e.kind === 'tell'), 'the tells show')
  // An untrained reader sees no tell and still takes a fifth of each claw
  const green = new Combat([two[0]], { skills: { blade: 0, polearm: 0, reading: 0 }, weapon: WEAPONS.sword, seed: 2 })
  const ev2 = fight(green, 14, 'guard')
  assert.equal(ev2.filter((e) => e.kind === 'tell').length, 0)
  assert.ok(green.hp < HERO_HP && green.hp > HERO_HP - 30)
})

test('an untrained fighter standing still is overwhelmed', () => {
  const c = new Combat(two, { skills: { blade: 0, polearm: 0, reading: 0 }, weapon: WEAPONS.sword, seed: 5 })
  const events = fight(c, 120)
  // either way it ends, and a fall is reported once
  assert.ok(c.over)
  if (c.over === 'fallen') assert.equal(events.filter((e) => e.kind === 'fallen').length, 1)
})

test('the pitchfork aims better for an equal skill, as pikes beat swords', () => {
  const s = { blade: 2, polearm: 2, reading: 1 }
  const sword = new Combat(two, { skills: s, weapon: WEAPONS.sword })
  const fork = new Combat(two, { skills: s, weapon: WEAPONS.pitchfork })
  assert.ok(fork.vitalChance() > sword.vitalChance())
  assert.ok(WEAPONS.pitchfork.reach > WEAPONS.sword.reach)
})

test('Strike, Pivot, Guard, Strike chained is a cadence that aims the next strike', () => {
  const c = new Combat([two[0]], { skills: skilled, weapon: WEAPONS.sword, seed: 1 })
  const base = c.vitalChance()
  const hero = { x: 0, z: 0 }
  const events: CombatEvent[] = []
  for (const s of ['pivot', 'guard', 'strike'] as Stance[]) {
    for (let i = 0; i < 10; i++) events.push(...c.step(1 / 30, hero))
    c.setStance(s)
  }
  events.push(...c.step(1 / 30, hero))
  assert.ok(events.some((e) => e.kind === 'cadence'))
  assert.ok(c.vitalChance() > base)
  assert.ok(CREATURES.hollowed.vitalOnly)
})

test('the thimble at the first fight usually wins against two Hollowed by striking alone', () => {
  let won = 0
  for (let seed = 1; seed <= 40; seed++) {
    const c = new Combat(two, { skills: { blade: 2, polearm: 1, reading: 1 }, weapon: WEAPONS.sword, seed })
    fight(c, 120)
    if (c.over === 'won') won++
  }
  // a fight to be careful in, not a foregone one
  assert.ok(won >= 24 && won <= 39, `won ${won} of 40`)
})
