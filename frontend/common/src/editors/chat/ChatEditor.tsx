import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { Alert, Stack, Typography } from '@mui/material'
import ChatComposer from '../../components/chat/ChatComposer.tsx'
import ChatSettingsPopover from '../../components/chat/ChatSettingsPopover.tsx'
import ChatTranscript from '../../components/chat/ChatTranscript.tsx'
import LevelToggle from '../../components/chat/LevelToggle.tsx'
import ModePicker from '../../components/chat/ModePicker.tsx'
import { modePickerShown, modeTitle } from '../../components/chat/modeMatch.ts'
import { editorBefore, offeredModes, startingMode, useModes } from '../../components/chat/modes.ts'
import { defaultLevel, useLevels } from '../../components/chat/levels.ts'
import { useChatStream, type ChatRound } from '../../components/chat/useChatStream.ts'
import { useContextSettings } from '../../components/chat/useContextSettings.ts'
import { log } from '../../logging/index.ts'
import { useSetting } from '../../settings/configStore.ts'
import { clearChatFocus, setChatFocus } from '../../chronicle/chatFocus.ts'
import { useWorkspace } from '../../workspace/workspaceStore.ts'
import { refreshChronicles } from '../../chronicle/chronicleStore.ts'
import { openSegment } from '../../components/chat/openSegment.ts'
import { useWorkbenchContext } from '../../workbench/context/WorkbenchContext.ts'
import { openWorkbenchLink, parseWorkbenchLink } from '../../workbench/links.ts'
import { useContributions } from '../../workbench/extensions/index.ts'
import { editors, type EditorProps } from '../../workbench/points.ts'
import type { ChatObject } from './ChatObject.ts'
import { PendingActivity } from '../../activity/ActivityLines.tsx'
import ActivityIndicator from '../../activity/ActivityIndicator.tsx'
import { chatClientTools, chatCommands, type ChatCommandContext, type ChatCommandContribution } from './points.ts'

// About four characters a token, for the level toggle's estimate
const tokensOf = (chars: number) => Math.ceil(chars / 4)

// AgentOS › Chat: a continuous scroll of conversations, streamed from the
// server with Chronicle memory (documentation/ChatCompletion.md,
// Chronicle.md)
// The data of the chronicle follower's followed event
interface ChronicleChanges {
  updated?: { id: string; title: string }[]
  proposed?: { id: string; title: string }[]
}

function ChatEditor({ tabId, registry }: EditorProps<ChatObject>) {
  // Only the tab's workspace's conversations (forge 16d)
  const ws = useWorkspace()
  const workspaceId = ws.current?.id
  const chat = useChatStream(workspaceId, ws.loaded)
  const places = useMemo(
    () => ({ workspaces: ws.workspaces.map((w) => ({ id: w.id, name: w.name })), current: workspaceId, onChange: chat.setWorkspaces }),
    [ws.workspaces, workspaceId, chat.setWorkspaces],
  )
  const { levels, models } = useLevels()
  const [levelId, setLevelId] = useState<string>()
  const level = levels.find((l) => l.id === levelId) ?? defaultLevel(levels)
  const commands = useContributions(chatCommands)
  const clientTools = useContributions(chatClientTools, { surface: 'chat' })
  const { contributions, defaults } = useContextSettings('chat')
  const store = useWorkbenchContext()
  const modes = useModes()
  const editorKinds = useContributions(editors)
  const [picking, setPicking] = useState(false) // the mode list, opened by a bare /mode (forge 15e)
  const messageBox = useRef<HTMLTextAreaElement>(null)
  // The list a bare /mode opened has closed: typing goes on in the message
  // box, not on the mode select (whose tooltip would cover the transcript)
  const donePicking = () => {
    setPicking(false)
    setTimeout(() => messageBox.current?.focus())
  }
  // The editor the user was in decides which modes are offered and the
  // mode a new conversation starts in (forge 06i)
  const before = useSyncExternalStore(store.subscribe, () => JSON.stringify(editorBefore(store.getState(), tabId)))
  const [chosenModes, setChosenModes] = useState<Record<string, string>>({})
  const systemPrompt = useSetting<string>('chat', 'systemPrompt', 'user') ?? ''
  const historyRounds = useSetting<number>('chat', 'historyRounds', 'user') ?? 0

  const { current } = chat
  const editor = useMemo(() => JSON.parse(before) as { kind?: string; subject?: string }, [before])
  const offered = useMemo(() => offeredModes(modes, editor), [modes, editor])
  const modeId = chosenModes[current.key] ?? current.conversation?.mode ?? startingMode(modes, editor)?.id
  const mode = modes.find((m) => m.id === modeId)
  // The ⚙ shows only the context settings the mode's graph uses
  const shownSettings = useMemo(
    () => (mode?.settings?.length ? contributions.filter((c) => mode.settings?.includes(c.id)) : contributions),
    [contributions, mode],
  )
  // The conversation's settings over the defaults, for every control shown
  const settings = useMemo(
    () => Object.fromEntries(contributions.map((c) => [c.id, { ...defaults[c.id], ...current.contextSettings[c.id] }])),
    [contributions, defaults, current.contextSettings],
  )
  const sendsWorkbench = settings.workbench !== undefined && settings.workbench.enabled !== false
  // The person's gestures since the last round go with the next message
  // while the mode gathers them and the conversation's switch is on (forge
  // 15d); a first message looks back as the server does
  const sendsActivity = settings.activity !== undefined && settings.activity.enabled !== false && shownSettings.some((c) => c.id === 'activity')
  const lastSent = current.rounds.findLast((r) => !r.note)?.time
  const lookBack = Number(settings.activity?.lookBackMinutes) || 30
  const activityAfter = useMemo(() => {
    if (lastSent) return lastSent
    const minute = 60_000
    return new Date(Math.floor((Date.now() - lookBack * minute) / minute) * minute).toISOString()
  }, [lastSent, lookBack, current.key]) // oxlint-disable-line react-hooks/exhaustive-deps

  // The Context view marks what the last round used and what its memory
  // update changed (the checks are the workspace's, forge 16c)
  const lastRound = current.rounds.findLast((r) => !r.note)
  const followed = lastRound?.followed?.filter((f) => f.follower === 'chronicle') ?? []
  const focusKey = JSON.stringify([
    current.key,
    current.conversation?.title,
    lastRound?.segments?.filter((s) => s.sourceId === 'chronicle' && s.pinned).map((s) => s.key),
    followed.flatMap((f) => [...((f.data as ChronicleChanges)?.updated ?? []), ...((f.data as ChronicleChanges)?.proposed ?? [])].map((e) => e.id)),
  ])
  useEffect(() => {
    const [key, title, chosen, changed] = JSON.parse(focusKey) as [string, string | undefined, string[] | undefined, string[]]
    setChatFocus({ owner: tabId, key, title, chosen: chosen ?? [], changed })
  }, [focusKey, tabId])
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

  // The person chose the mode since the last message (the menu or /mode),
  // so the pre-mode leaves the next one alone (forge 15f)
  const modeChosen = useRef(false)
  const sendWith = (text: string, mode: string | undefined, extra: { modeChosen?: boolean; undoes?: string } = {}) => {
    const key = current.key
    const chosen = extra.modeChosen ?? modeChosen.current
    modeChosen.current = false
    void chat.send(text, {
      level: level?.id,
      mode,
      workbench: sendsWorkbench ? store.snapshot('main', tabId) : undefined,
      contextSettings: settings,
      // chat.askChoice is always declared: the pre-mode asks on its card, even with Tools off
      clientTools: settings.tools?.enabled === false ? clientTools.filter((t) => t.id === 'chat.askChoice') : clientTools,
      toolContext: { registry, store, tabId },
      offered: offered.map((m) => m.id),
      modeChosen: chosen,
      undoes: extra.undoes,
      workspace: workspaceId, // the server reads its active Volumes and entries (forge 16c)
      onMode: (id) => setChosenModes((m) => ({ ...m, [key]: id })),
    })
  }
  const send = (text: string) => sendWith(text, modeId)

  // Undo of a pre-mode switch: the message runs again in the mode it left,
  // and the switched round leaves the history (forge 15f)
  const undoSwitch = (round: ChatRound) => {
    const from = round.preMode?.from
    if (!from || chat.pending) return
    setChosenModes((m) => ({ ...m, [current.key]: from }))
    sendWith(round.text, from, { modeChosen: true, undoes: round.id })
  }

  // A switch from the menu or /mode: at once for this conversation, and
  // stored on the server once it exists (forge 15e)
  const chooseMode = (id: string) => {
    modeChosen.current = true
    setChosenModes((m) => ({ ...m, [current.key]: id }))
    if (current.conversation && current.conversation.mode !== id) void chat.setMode(id)
  }

  // New mode… and /mode new switch to the mode that makes modes (forge 15h)
  const maker = modes.find((m) => m.makesModes)
  const newMode = () => {
    if (!maker) return false
    if (picking) donePicking()
    chooseMode(maker.id)
    chat.say(`Switched to ${modeTitle(maker)}: tell it what the new mode is for.`)
    return true
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
      contextSettings: settings,
      setContextSetting: chat.setContextSetting,
      modes: offered,
      allModes: modes,
      mode,
      setMode: chooseMode,
      newMode,
      pickMode: () => {
        if (chat.pending || !modePickerShown(offered, mode)) return false
        setPicking(true)
        return true
      },
      kindName: (kind) => editorKinds.find((e) => e.id === kind)?.name,
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
        modes={modes}
        onLoadEarlier={() => void chat.loadEarlier()}
        onNewConversation={chat.newConversation}
        onFeedback={(round, feedback) => void chat.setFeedback(round, feedback)}
        onAnswerTool={chat.answerTool}
        onUndoSwitch={chat.pending ? undefined : undoSwitch}
        clientTools={clientTools}
        onOpenLink={(href) => {
          if (!parseWorkbenchLink(href)) return false
          openWorkbenchLink(registry, href).catch((err: unknown) => log.In('chat').fromError(err).With('href', href).warn('cannot open the link'))
          return true
        }}
        onOpenSegment={(segment) => void openSegment(registry, segment)}
        tail={sendsActivity && !chat.pending ? <PendingActivity after={activityAfter} /> : undefined}
        places={places}
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
        inputRef={messageBox}
        tools={
          <>
            <ModePicker
              modes={offered}
              value={mode}
              open={picking || undefined}
              onOpenChange={(open) => !open && picking && donePicking()}
              onChange={(id) => {
                if (picking && id !== modeId) {
                  const picked = modes.find((m) => m.id === id)
                  if (picked) chat.say(`Switched to ${modeTitle(picked)}`)
                }
                if (picking) donePicking()
                chooseMode(id)
              }}
              disabled={chat.pending}
              onNewMode={maker ? newMode : undefined}
              onEditMode={(m) => {
                const href = `workbench:graph/${m.graphRef}`
                openWorkbenchLink(registry, href).catch((err: unknown) => log.In('chat').fromError(err).With('href', href).warn('cannot open the mode graph'))
              }}
            />
            <LevelToggle levels={levels} models={models} value={level?.id} onChange={setLevelId} contextTokens={contextTokens} />
            <ChatSettingsPopover contributions={shownSettings} values={settings} onChange={chat.setContextSetting} />
            {sendsActivity && <ActivityIndicator />}
          </>
        }
      />
    </Stack>
  )
}

export default ChatEditor
