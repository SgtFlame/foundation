import { Suspense, useEffect, useState } from 'react'
import { Box, CircularProgress, Paper, Typography } from '@mui/material'
import { useContributions } from '../../workbench/extensions/index.ts'
import type { EditorProps } from '../../workbench/points.ts'
import type { DashboardObject } from './DashboardObject.ts'
import { dashboardWidgets } from './points.ts'

// How often the widgets reload while the Dashboard is open
const refreshMs = 60_000

// AgentOS › Dashboard: a responsive grid of dashboard.widget contributions,
// refreshed on focus and every minute
function DashboardEditor({ registry }: EditorProps<DashboardObject>) {
  const widgets = useContributions(dashboardWidgets)
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    const bump = () => setRefresh((n) => n + 1)
    const timer = window.setInterval(bump, refreshMs)
    window.addEventListener('focus', bump)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', bump)
    }
  }, [])

  return (
    <Box sx={{ height: '100%', overflow: 'auto', p: 2, boxSizing: 'border-box' }}>
      <Box sx={{ display: 'grid', gap: 2, gridTemplateColumns: { xs: '1fr', lg: 'repeat(2, minmax(0, 1fr))' }, maxWidth: 1400, mx: 'auto' }}>
        {widgets.map((w) => (
          <Paper key={w.id} variant="outlined" sx={{ p: 2, gridColumn: { lg: w.size === 'full' ? '1 / -1' : 'auto' }, minWidth: 0 }}>
            <Typography variant="h6" component="h2" sx={{ mb: 1.5 }}>
              {w.title}
            </Typography>
            <Suspense fallback={<CircularProgress size={24} />}>
              <w.component refresh={refresh} registry={registry} />
            </Suspense>
          </Paper>
        ))}
      </Box>
    </Box>
  )
}

export default DashboardEditor
