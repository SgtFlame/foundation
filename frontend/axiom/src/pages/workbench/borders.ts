// Builds the workbench borders from workbench.borderTab contributions and
// keeps them in step as contributions come and go.

import { Actions, DockLocation, type IJsonBorderNode, type IJsonTabNode, type Model } from 'flexlayout-react'
import { log } from '../../logging/index.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { borderTabs, type BorderLocation, type BorderTabContribution } from '../../workbench/points.ts'

const locations: readonly BorderLocation[] = ['left', 'right', 'top', 'bottom']

const tabId = (c: BorderTabContribution) => `border:${c.id}`

function tabJson(c: BorderTabContribution): IJsonTabNode {
  return {
    type: 'tab',
    id: tabId(c),
    name: c.name,
    helpText: c.helpText,
    component: 'borderTab',
    config: { contributionId: c.id },
    enableClose: false,
  }
}

// A border for each location that has tabs, collapsed. flexlayout draws empty
// borders, so a location with none gets no border.
export function bordersJson(): IJsonBorderNode[] {
  const tabs = extensions.get(borderTabs)
  return locations.flatMap((location): IJsonBorderNode[] => {
    const here = tabs.filter((c) => c.location === location)
    if (here.length === 0) return []
    return [
      {
        type: 'border',
        location,
        selected: -1,
        size: Math.max(...here.map((c) => c.size ?? 0)) || 320,
        children: here.map(tabJson),
      },
    ]
  })
}

// Adds tabs for new contributions and removes those whose contribution went.
// Borders cannot be added to a live model, so a tab for a location with no
// border yet appears on the next load.
export function syncBorders(model: Model) {
  const tabs = extensions.get(borderTabs)
  for (const c of tabs) {
    if (model.getNodeById(tabId(c))) continue
    if (!model.getNodeById(`border_${c.location}`)) {
      log.In('workbench').With('id', c.id, 'location', c.location).warn('%s waits for a reload: no %s border yet', c.id, c.location)
      continue
    }
    model.doAction(Actions.addNode(tabJson(c), `border_${c.location}`, DockLocation.CENTER, -1, false))
  }
  const wanted = new Set(tabs.map(tabId))
  for (const location of locations) {
    for (const node of model.getNodeById(`border_${location}`)?.getChildren() ?? []) {
      const id = node.getId()
      if (id.startsWith('border:') && !wanted.has(id)) model.doAction(Actions.deleteTab(id))
    }
  }
}
