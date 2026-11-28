// mui.metric (forge 40a): one number shown large and formatted, with the
// change from a compare value; the Stat tile's value. It counts to a new
// value over a short animation, so a live aggregate's update reads as
// movement.

import { useEffect, useRef, useState } from 'react'
import { Box, Stack, Typography } from '@mui/material'
import ArrowDropUpIcon from '@mui/icons-material/ArrowDropUp'
import ArrowDropDownIcon from '@mui/icons-material/ArrowDropDown'
import type { RendererProps } from '../../../points.ts'
import { deltaOf, formatValue, num, type Format } from '../../../../components/charts/chartData.ts'

const FORMATS: Format[] = ['number', 'money', 'percent', 'duration']
const VARIANT = { small: 'h6', medium: 'h4', large: 'h3' } as const

// The value shown, eased towards the target over 400 ms
function useTween(target: number): number {
  const [shown, setShown] = useState(target)
  const from = useRef(target)
  useEffect(() => {
    const start = performance.now()
    const a = from.current
    if (a === target || typeof requestAnimationFrame !== 'function') {
      from.current = target
      setShown(target)
      return
    }
    let frame = 0
    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / 400)
      const v = a + (target - a) * (1 - (1 - t) ** 3)
      from.current = v
      setShown(v)
      if (t < 1) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [target])
  return shown
}

export default function MuiMetric(p: RendererProps) {
  const props = p.props
  const format: Format = FORMATS.includes(props.format as Format) ? (props.format as Format) : 'number'
  const currency = typeof props.currency === 'string' && props.currency ? props.currency : 'USD'
  const size = props.size === 'small' || props.size === 'large' ? props.size : 'medium'
  const missing = props.value === undefined || props.value === null || props.value === ''
  const target = num(props.value)
  const shown = useTween(target)
  const delta = deltaOf(props.value, props.compare)
  const bad = props.upIsBad === true
  const color = !delta || delta.direction === 'flat' ? 'text.secondary' : (delta.direction === 'up') !== bad ? 'success.main' : 'error.main'
  return (
    <Box sx={props.sx as object}>
      <Typography variant={VARIANT[size]} component="div" sx={{ fontVariantNumeric: 'tabular-nums' }} aria-live="polite">
        {missing ? '—' : formatValue(shown === target ? target : shown, format, currency)}
      </Typography>
      {delta && (
        <Stack direction="row" spacing={0.5} sx={{ color, alignItems: "center" }}>
          {delta.direction === 'up' && <ArrowDropUpIcon fontSize="small" />}
          {delta.direction === 'down' && <ArrowDropDownIcon fontSize="small" />}
          <Typography variant="body2" component="span">
            {delta.fraction === null ? formatValue(target - num(props.compare), format, currency) : formatValue(delta.fraction, 'percent')}
          </Typography>
          {typeof props.compareLabel === 'string' && props.compareLabel && (
            <Typography variant="body2" component="span" color="text.secondary">{props.compareLabel}</Typography>
          )}
        </Stack>
      )}
    </Box>
  )
}
