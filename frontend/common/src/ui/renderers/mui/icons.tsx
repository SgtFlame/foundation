// The Material icons server-described trees may name. A name not listed
// renders through MUI's Icon (the Material Icons font, if the app loads it).

import { Icon, type SvgIconProps } from '@mui/material'
import { icons } from './iconMap.ts'

export function NamedIcon({ name, ...props }: { name?: string } & SvgIconProps) {
  if (!name) return null
  const Svg = icons[name]
  if (Svg) return <Svg {...props} />
  return (
    <Icon fontSize={props.fontSize === 'inherit' ? 'inherit' : props.fontSize} color={props.color as never}>
      {name}
    </Icon>
  )
}
