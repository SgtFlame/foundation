// The categorical palette of Dashboard charts, in its fixed order (never
// cycled): eight hues validated for colour-blind separation on the adjacent
// pairs a stacked bar shows, with steps chosen separately for light and dark
// surfaces. A ninth series folds into Other, which is neutral grey.

const light = ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4', '#008300', '#6250d6', '#e34948']
const dark = ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181', '#008300', '#9085e9', '#e66767']

export const maxSeries = 7 // plus Other, so a stack never needs a ninth colour

export function seriesColors(mode: 'light' | 'dark'): readonly string[] {
  return mode === 'dark' ? dark : light
}

export function otherColor(mode: 'light' | 'dark'): string {
  return mode === 'dark' ? '#7c7b76' : '#a3a29d'
}
