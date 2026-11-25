// Workspaces from the chat (forge 16a question 7): /workspace, and the
// workspace.switch client tool. Making, renaming and deleting are the
// server's command tools (workspace.create, ...), which ask first;
// switching only changes what this tab shows.
import { chatClientTools, chatCommands } from '../editors/chat/points.ts'
import { extensions } from '../workbench/extensions/index.ts'
import { matchWorkspace, nameProblem, workspaceList } from './current.ts'
import { workspaces } from './workspaceStore.ts'

extensions.contribute(chatCommands, {
  id: 'workspace',
  source: 'workspace',
  order: 320,
  description: 'Switch this tab to another workspace, list them, or make one (/workspace new <name>)',
  usage: '[name | new <name>]',
  run: (arg, ctx) => {
    workspaces.start()
    const { workspaces: list, current, loaded } = workspaces.get()
    if (!loaded) {
      ctx.say('Your workspaces are still loading. Try again in a moment.')
      return
    }
    const query = arg.trim()
    if (!query) {
      ctx.say(`Your workspaces:\n\n${workspaceList(list, current?.id)}\n\nMake one with \`/workspace new <name>\`.`)
      return
    }
    const made = /^new(?:\s+(.*))?$/i.exec(query)
    if (made) {
      const name = (made[1] ?? '').trim()
      const problem = name ? nameProblem(name, list) : 'Give it a name: `/workspace new <name>`'
      if (problem) {
        ctx.say(problem)
        return
      }
      workspaces
        .createAndSwitch(name)
        .then(() => {
          ctx.say(`Made the workspace ${name}, now shown in this tab.`)
        })
        .catch((err: unknown) => ctx.say(`The workspace was not made: ${err instanceof Error ? err.message : String(err)}`))
      return
    }
    const match = matchWorkspace(query, list)
    switch (match.kind) {
      case 'found':
        if (match.workspace.id === current?.id) ctx.say(`This tab already shows ${match.workspace.name}.`)
        else {
          workspaces.switchTo(match.workspace.id)
          ctx.say(`Switched to the workspace ${match.workspace.name}.`)
        }
        return
      case 'ambiguous':
        ctx.say(`\`${query}\` could be ${match.workspaces.map((w) => w.name).join(' or ')}. Type more of its name.`)
        return
      case 'unknown':
        ctx.say(`There is no workspace \`${query}\`. Your workspaces:\n\n${workspaceList(list, current?.id)}`)
    }
  },
})

extensions.contribute(chatClientTools, {
  id: 'workspace.switch',
  source: 'workspace',
  order: 200,
  title: 'Switch workspace',
  description:
    "Switches this browser tab to another of the user's workspaces, by id (from workspace.list or workspace.create's target) or by name. Only what the tab shows changes; nothing is saved or deleted.",
  input: {
    type: 'object',
    properties: {
      id: { type: 'string', description: 'The workspace id, e.g. default' },
      name: { type: 'string', description: 'Its name, when you do not have the id' },
    },
  },
  effect: 'ui',
  run: async (args) => {
    workspaces.start()
    let { workspaces: list } = workspaces.get()
    const id = typeof args.id === 'string' ? args.id : ''
    // a workspace the model just made may not have reached this browser yet
    for (let i = 0; i < 20 && id && !list.some((w) => w.id === id); i++) {
      await new Promise((r) => setTimeout(r, 100))
      list = workspaces.get().workspaces
    }
    let target = list.find((w) => w.id === id)
    if (!target && typeof args.name === 'string') {
      const match = matchWorkspace(args.name, list)
      if (match.kind === 'ambiguous') throw new Error(`${args.name} could be ${match.workspaces.map((w) => w.name).join(' or ')}`)
      if (match.kind === 'found') target = match.workspace
    }
    if (!target) throw new Error(`there is no such workspace; the user's are ${list.map((w) => `${w.name} (${w.id})`).join(', ')}`)
    workspaces.switchTo(target.id)
    return { switched: true, id: target.id, name: target.name }
  },
})
