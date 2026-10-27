import { useEffect, useMemo, useState } from 'react'
import { Alert, Stack, Typography } from '@mui/material'
import ChatComposer from '../../components/chat/ChatComposer.tsx'
import ChatSettingsPopover from '../../components/chat/ChatSettingsPopover.tsx'
import ChatTranscript from '../../components/chat/ChatTranscript.tsx'
import LevelToggle from '../../components/chat/LevelToggle.tsx'
import { defaultLevel, useLevels } from '../../components/chat/levels.ts'
import { useChatStream } from '../../components/chat/useChatStream.ts'
import { useContextSettings } from '../../components/chat/useContextSettings.ts'
import { log } from '../../logging/index.ts'
import { useSetting } from '../../settings/configStore.ts'
import { clearChatFocus, setChatFocus } from '../../chronicle/chatFocus.ts'
import { refreshChronicles } from '../../chronicle/chronicleStore.ts'
import { ChronicleObject } from '../chronicle/ChronicleObject.ts'
import { openMarkdownAt } from '../markdown/openMarkdown.ts'
import { useWorkbenchContext } from '../../workbench/context/WorkbenchContext.ts'
import { useContributions } from '../../workbench/extensions/index.ts'
import type { EditorProps } from '../../workbench/points.ts'
import type { ChatObject } from './ChatObject.ts'
import { chatClientTools, chatCommands, type ChatCommandContext, type ChatCommandContribution } from './points.ts'

// About four characters a token, for the level toggle's estimate
const tokensOf = (chars: number) => Math.ceil(chars / 4)

// AgentOS › Chat: a continuous scroll of conversations, streamed from the
// server with Chronicle memory (documentation/plans/ChatCompletion.md,
// Chronicle.md)
// The data of the chronicle follower's followed event
interface ChronicleChanges {
  updated?: { id: string; title: string }[]
  proposed?: { id: string; title: string }[]
}

function ChatEditor({ tabId, registry }: EditorProps<ChatObject>) {
  const chat = useChatStream()
  const { levels, models } = useLevels()
  const [levelId, setLevelId] = useState<string>()
  const level = levels.find((l) => l.id === levelId) ?? defaultLevel(levels)
  const commands = useContributions(chatCommands)
  const clientTools = useContributions(chatClientTools, { surface: 'chat' })
  const { contributions, defaults } = useContextSettings('chat')
  const store = useWorkbenchContext()
  const systemPrompt = useSetting<string>('chat', 'systemPrompt', 'user') ?? ''
  const historyRounds = useSetting<number>('chat', 'historyRounds', 'user') ?? 0

  const { current } = chat
  // The conversation's settings over the defaults, for every control shown
  const settings = useMemo(
    () => Object.fromEntries(contributions.map((c) => [c.id, { ...defaults[c.id], ...current.contextSettings[c.id] }])),
    [contributions, defaults, current.contextSettings],
  )
  const sendsWorkbench = settings.workbench !== undefined && settings.workbench.enabled !== false

  // The Chronicles view's checkboxes act on the current conversation: it
  // sees what is checked, what the last round used and what its memory
  // update changed
  const { setContextSetting } = chat
  const chronicle = settings.chronicle
  const lastRound = current.rounds.findLast((r) => !r.note)
  const followed = lastRound?.followed?.filter((f) => f.follower === 'chronicle') ?? []
  const focusKey = JSON.stringify([
    current.key,
    current.conversation?.title,
    chronicle?.checked,
    lastRound?.segments?.filter((s) => s.sourceId === 'chronicle' && s.pinned).map((s) => s.key),
    followed.flatMap((f) => [...((f.data as ChronicleChanges)?.updated ?? []), ...((f.data as ChronicleChanges)?.proposed ?? [])].map((e) => e.id)),
  ])
  useEffect(() => {
    const [key, title, checked, chosen, changed] = JSON.parse(focusKey) as [string, string | undefined, string[] | undefined, string[] | undefined, string[]]
    setChatFocus({
      owner: tabId, key, title, checked: checked ?? [], chosen: chosen ?? [], changed,
      setChecked: (ids) => setContextSetting('chronicle', { ...chronicle, checked: ids }),
    })
  }, [focusKey, tabId, chronicle, setContextSetting])
  useEffect(() => () => clearChatFocus(tabId), [tabId])
  // A memory update changed entries: the Chronicles view shows them
  useEffect(() => {
    if (followed.length > 0) void refreshChronicles()
  }, [followed.length])

  // What the next message sends: the system prompt, the history the server
  // will include, and the snapshot
  const history = current.rounds.filter((r) => !r.note && r.reply && r.feedback !== 'ignore' && (r.status === 200 || r.status === 499))
  const sent = historyRounds > 0 ? history.slice(-historyRounds) : history
  const contextTokens = tokensOf(systemPrompt.length + sent.reduce((n, r) => n + r.text.length + r.reply.length, 0) + (sendsWorkbench ? 1500 : 0))

  const send = (text: string) => {
    void chat.send(text, {
      level: level?.id,
      workbench: sendsWorkbench ? store.snapshot('main', tabId) : undefined,
      contextSettings: settings,
      clientTools: settings.tools?.enabled === false ? undefined : clientTools,
      toolContext: { registry, store, tabId },
    })
  }

  const runCommand = (command: ChatCommandContribution, arg: string) => {
    const ctx: ChatCommandContext = {
      newConversation: chat.newConversation,
      setLevel: (id) => {
        const found = levels.find((l) => l.id === id || l.label.toLowerCase() === id.toLowerCase())
        if (found) setLevelId(found.id)
        return !!found
      },
      levels,
      say: chat.say,
      commands,
    }
    command.run(arg, ctx)
  }

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <ChatTranscript
        threads={chat.threads}
        more={chat.more}
        loading={chat.loading}
        levels={levels}
        onLoadEarlier={() => void chat.loadEarlier()}
        onNewConversation={chat.newConversation}
        onFeedback={(round, feedback) => void chat.setFeedback(round, feedback)}
        onAnswerTool={chat.answerTool}
        clientTools={clientTools}
        onOpenSegment={(segment) => {
          const { entryId, path, startLine, endLine } = segment.location
          if (entryId) {
            registry.edit(new ChronicleObject({ id: entryId, title: segment.title }))
            return
          }
          if (!path) return
          openMarkdownAt(registry, path, startLine ? [startLine, endLine ?? startLine] : undefined).catch((err: unknown) =>
            log.In('chat').fromError(err).With('path', path).warn('cannot open the cited file'),
          )
        }}
        empty={
          <Stack sx={{ height: '100%', boxSizing: 'border-box', alignItems: 'center', justifyContent: 'center', p: 4, textAlign: 'center' }} spacing={1}>
            <Typography variant="h6">Ask anything</Typography>
            <Typography variant="body2" color="text.secondary">
              The assistant sees which editors you have open, so you can ask about "this", remembers through your Chronicles, and searches your Volumes, citing what it uses. It can use tools to look further and to propose changes, which you approve. Type / for commands.
            </Typography>
          </Stack>
        }
      />
      {chat.error && (
        <Alert severity="warning" sx={{ mx: 1 }}>
          {chat.error}
        </Alert>
      )}
      <ChatComposer
        onSend={send}
        onCommand={runCommand}
        onStop={chat.stop}
        pending={chat.pending}
        commands={commands}
        tools={
          <>
            <LevelToggle levels={levels} models={models} value={level?.id} onChange={setLevelId} contextTokens={contextTokens} />
            <ChatSettingsPopover contributions={contributions} values={settings} onChange={chat.setContextSetting} />
          </>
        }
      />
    </Stack>
  )
}

export default ChatEditor
