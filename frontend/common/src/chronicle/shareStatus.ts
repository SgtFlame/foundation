// What each state of a global share request means to its owner

import type { ShareStatus } from '../services/Chronicle.ts'

export const shareStatusText: Record<ShareStatus, string> = {
  pending: 'Waiting for a moderator of your tenant',
  tenantApproved: 'Approved by your tenant; waiting for a global moderator',
  approved: 'Shared with every tenant',
  rejected: 'Rejected',
  withdrawn: 'Withdrawn',
}
