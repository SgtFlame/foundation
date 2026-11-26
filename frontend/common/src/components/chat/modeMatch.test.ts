// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { ChatMode } from '../../services/Conversation.ts'
import { matchMode, modeMatchMessage, normalizeModeName, whereOffered } from './modeMatch.ts'

const mode = (id: string, label: string, extra: Partial<ChatMode> = {}): ChatMode => ({ id, label, graphRef: id, ...extra })
const personal = mode('personal', 'Personal', { icon: '💬', default: true, description: 'Chat with memory' })
const agents = mode('forge-agent-builder', 'Agent Builder', { icon: '🤖', focus: { kinds: ['app:*'], offerOnly: true }, description: 'Make an agent' })
const apps = mode('forge-builder', 'App Builder', { icon: '🛠️', focus: { kinds: ['prefab', 'blueprint'], subjects: ['prefab'] } })
const all = [personal, agents, apps]
const kindName = (k: string) => ({ prefab: 'Prefab', blueprint: 'Blueprint' })[k]

test('names are compared without case, emoji, spaces or punctuation', () => {
  assert.equal(normalizeModeName('🤖 Agent  Builder!'), 'agentbuilder')
})

test('a label matches ignoring case, emoji and spaces', () => {
  const offered = [personal, agents]
  for (const q of ['agent builder', 'AgentBuilder', '🤖 agent builder', 'AGENT-BUILDER']) {
    assert.deepEqual(matchMode(q, offered, all), { kind: 'found', mode: agents }, q)
  }
})

test('an id matches', () => {
  assert.deepEqual(matchMode('forge-agent-builder', [personal, agents], all), { kind: 'found', mode: agents })
  assert.deepEqual(matchMode('personal', [personal, agents], all), { kind: 'found', mode: personal })
})

test('a unique prefix is enough', () => {
  assert.deepEqual(matchMode('agent', [personal, agents], all), { kind: 'found', mode: agents })
  assert.deepEqual(matchMode('pers', [personal, agents], all), { kind: 'found', mode: personal })
})

test('a prefix of two offered modes is ambiguous', () => {
  const offered = [personal, agents, apps]
  const amb = matchMode('forge', offered, all)
  assert.equal(amb.kind, 'ambiguous')
  assert.deepEqual(amb.kind === 'ambiguous' && amb.modes.map((x) => x.id), ['forge-agent-builder', 'forge-builder'])
})

test('an exact name wins over a longer one it begins', () => {
  const chat = mode('chat', 'Chat')
  const chatPlus = mode('chat-plus', 'Chat plus')
  assert.deepEqual(matchMode('chat', [chat, chatPlus], [chat, chatPlus]), { kind: 'found', mode: chat })
})

test('a mode not offered here is named as such, never switched to', () => {
  assert.deepEqual(matchMode('app builder', [personal, agents], all), { kind: 'notOffered', mode: apps })
  assert.deepEqual(matchMode('app', [personal, agents], all), { kind: 'notOffered', mode: apps })
  // an exact name elsewhere wins over a prefix here
  assert.deepEqual(matchMode('app builder', [personal, mode('app-builder-two', 'App builder two')], all), { kind: 'notOffered', mode: apps })
  assert.deepEqual(matchMode('agent builder', [personal], all), { kind: 'notOffered', mode: agents })
})

test('a missing graph is not switched to', () => {
  const gone = mode('gone', 'Gone', { missing: true })
  assert.equal(matchMode('gone', [personal, gone], [personal, gone]).kind, 'unknown')
})

test('an unknown name lists the modes here with their descriptions', () => {
  const offered = [personal, agents]
  const m = matchMode('nothing', offered, all)
  assert.equal(m.kind, 'unknown')
  const text = modeMatchMessage('nothing', m, offered, kindName)
  assert.match(text, /There is no mode `nothing`/)
  assert.match(text, /`\/mode agent builder`: 🤖 Agent Builder, Make an agent/)
  assert.match(text, /`\/mode personal`: 💬 Personal, Chat with memory/)
})

test('where a mode is offered, from its focus', () => {
  assert.equal(whereOffered(agents, kindName), "on any app's tab")
  assert.equal(whereOffered(apps, kindName), 'on Prefab or Blueprint tabs')
  // forge 18f: offerIn adds places, '*' everywhere
  assert.equal(whereOffered({ ...apps, focus: { ...apps.focus, offerIn: ['blueprint'] } }, kindName), 'on Prefab or Blueprint tabs')
  assert.equal(whereOffered({ ...agents, focus: { kinds: ['app:requirements:*'], offerIn: ['*'] } }, kindName), 'everywhere')
  assert.equal(whereOffered(personal, kindName), 'everywhere')
  assert.equal(whereOffered(mode('x', 'X', { focus: { subjects: ['graph'] } }), kindName), 'on graph tabs')
  const text = modeMatchMessage('app builder', { kind: 'notOffered', mode: apps }, [personal], kindName)
  assert.match(text, /🛠️ App Builder is not offered on this tab\. It is offered on Prefab or Blueprint tabs\./)
})
