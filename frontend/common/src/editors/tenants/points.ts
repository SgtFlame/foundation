// Extension points owned by Administration › Tenants

import { defineExtensionPoint, type Contribution } from '../../workbench/extensions/index.ts'
import type { TenantSummary } from '../../services/Tenants.ts'

// An action on the selected tenant, such as an app seeding its defaults.
// It is confirmed before it runs; what run resolves to is shown after.
export interface TenantActionContribution extends Contribution {
  title: string
  confirm: { title: string; text: string; label?: string }
  run(tenant: TenantSummary): Promise<string | void>
  // when(ctx) sees { can, tenantId } as the Editors view's commands do
}

export const tenantActions = defineExtensionPoint<TenantActionContribution>(
  'tenants.action',
  'Actions on the selected tenant in Administration › Tenants, each confirmed before it runs',
)
