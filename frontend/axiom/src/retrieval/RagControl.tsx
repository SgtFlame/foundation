import { Chip, FormControlLabel, Slider, Stack, Switch, Typography } from '@mui/material'
import type { ContextControlProps } from '../editors/chat/points.ts'
import { useSetting } from '../settings/configStore.ts'

// A named position of the dials, from Settings (chat.context.presets)
interface Preset {
  id: string
  title: string
  lexicalSemantic: number
  chronicleLibrary: number
  topK: number
}

const axisMarks = Array.from({ length: 11 }, (_, i) => ({ value: i - 5 }))

function Dial({
  label,
  left,
  right,
  value,
  onChange,
  disabled,
}: {
  label: string
  left: string
  right: string
  value: number
  onChange: (value: number) => void
  disabled: boolean
}) {
  return (
    <Stack>
      <Typography variant="caption" id={`dial-${label}`}>
        {label}
      </Typography>
      <Slider
        size="small"
        min={-5}
        max={5}
        step={1}
        marks={axisMarks}
        value={value}
        disabled={disabled}
        track={false}
        valueLabelDisplay="auto"
        onChange={(_, v) => onChange(v as number)}
        aria-labelledby={`dial-${label}`}
        getAriaValueText={(v) => (v === 0 ? 'neutral' : v < 0 ? `${-v} towards ${left}` : `${v} towards ${right}`)}
      />
      <Stack direction="row" sx={{ justifyContent: 'space-between', mt: -1 }}>
        <Typography variant="caption" color="text.secondary">
          {left}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {right}
        </Typography>
      </Stack>
    </Stack>
  )
}

// RAG Search: whether retrieval runs for the conversation, the preset chips,
// the two dials (documentation/ContextRetrieval.md) and Top-K, the
// most segments injected. The server's "rag" context provider reads them.
function RagControl({ value, onChange }: ContextControlProps) {
  const presets = useSetting<Preset[]>('chat.context.presets', 'presets', 'user') ?? []
  const enabled = value.enabled !== false
  const lexical = Number(value.lexicalSemantic ?? 0)
  const balance = Number(value.chronicleLibrary ?? 0)
  const topK = Number(value.topK ?? 8)
  const sources = (value.sources ?? {}) as Record<string, boolean>
  const preset = presets.find((p) => p.id === value.preset)
  // Moving a dial makes it Custom
  const set = (patch: Record<string, unknown>) => onChange({ ...value, preset: '', ...patch })

  return (
    <Stack spacing={1}>
      <FormControlLabel
        control={<Switch size="small" checked={enabled} onChange={(_, on) => onChange({ ...value, enabled: on })} />}
        label="Search Volumes and Chronicles"
      />
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5 }}>
        {presets.map((p) => (
          <Chip
            key={p.id}
            size="small"
            label={p.title}
            disabled={!enabled}
            color={preset?.id === p.id ? 'primary' : 'default'}
            variant={preset?.id === p.id ? 'filled' : 'outlined'}
            onClick={() => onChange({ ...value, preset: p.id, lexicalSemantic: p.lexicalSemantic, chronicleLibrary: p.chronicleLibrary, topK: p.topK })}
          />
        ))}
        <Chip size="small" label="Custom" disabled={!enabled} variant={preset ? 'outlined' : 'filled'} color={preset ? 'default' : 'primary'} />
      </Stack>
      <Dial label="Lexical ↔ Semantic" left="Exact keywords" right="Meaning" value={lexical} disabled={!enabled} onChange={(v) => set({ lexicalSemantic: v })} />
      <Dial label="Chronicles ↔ Library" left="Memory" right="Volumes" value={balance} disabled={!enabled} onChange={(v) => set({ chronicleLibrary: v })} />
      <Stack>
        <Typography variant="caption" id="rag-topk">
          Segments: {topK}
        </Typography>
        <Slider
          size="small"
          min={1}
          max={50}
          value={topK}
          disabled={!enabled}
          onChange={(_, v) => set({ topK: v as number })}
          aria-labelledby="rag-topk"
        />
      </Stack>
      <Stack direction="row">
        {(
          [
            ['library', 'Volumes'],
            ['chronicle', 'Chronicles'],
          ] as const
        ).map(([id, label]) => (
          <FormControlLabel
            key={id}
            control={
              <Switch
                size="small"
                checked={sources[id] !== false}
                disabled={!enabled}
                onChange={(_, on) => onChange({ ...value, sources: { ...sources, [id]: on } })}
              />
            }
            label={label}
          />
        ))}
      </Stack>
      <Typography variant="caption" color="text.secondary">
        The best-matching passages and memory entries go with each message, numbered, so the reply can cite them.
      </Typography>
    </Stack>
  )
}

export default RagControl
