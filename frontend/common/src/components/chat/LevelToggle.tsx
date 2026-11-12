import { ToggleButton, ToggleButtonGroup, Tooltip } from '@mui/material'
import { formatCost, formatTokens, type CatalogModel, type ChatLevel } from './levels.ts'

// One button per chat level; hovering shows the level's model and what
// sending the current context would cost before the reply
function LevelToggle({
  levels,
  models,
  value,
  onChange,
  contextTokens,
  disabled,
}: {
  levels: readonly ChatLevel[]
  models: readonly CatalogModel[]
  value?: string
  onChange: (id: string) => void
  contextTokens: number // an estimate of what the next message sends
  disabled?: boolean
}) {
  return (
    <ToggleButtonGroup
      size="small"
      exclusive
      value={value ?? null}
      onChange={(_, id: string | null) => id && onChange(id)}
      disabled={disabled}
      aria-label="Chat level"
    >
      {levels.map((level) => {
        const model = models.find((m) => m.id === level.modelRef)
        const pricing = model?.pricing
        const cost = pricing?.inputPerMTok !== undefined ? formatCost(contextTokens * pricing.inputPerMTok, pricing.currency) : undefined
        const hint = [
          `${level.label}: ${model?.title ?? model?.model ?? level.modelRef}`,
          `≈ ${formatTokens(contextTokens)} tokens in${cost ? `, ≈ ${cost}` : ''} before the reply`,
        ].join(' · ')
        return (
          <Tooltip key={level.id} title={hint}>
            <ToggleButton value={level.id} aria-label={level.label} sx={{ px: 1, py: 0.25, textTransform: 'none' }}>
              {level.icon ? `${level.icon} ` : ''}
              {level.label}
            </ToggleButton>
          </Tooltip>
        )
      })}
    </ToggleButtonGroup>
  )
}

export default LevelToggle
