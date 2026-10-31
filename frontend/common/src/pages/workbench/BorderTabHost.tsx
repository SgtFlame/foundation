import { Box, CircularProgress, Typography } from '@mui/material'
import { Suspense } from 'react'
import { useContributions } from '../../workbench/extensions/index.ts'
import { borderTabs } from '../../workbench/points.ts'

// Renders the workbench.borderTab contribution a border tab was made for
function BorderTabHost({ id }: { id: string }) {
  const tab = useContributions(borderTabs).find((c) => c.id === id)
  if (!tab) {
    return (
      <Box sx={{ p: 2 }}>
        <Typography color="text.secondary">This tab is not available.</Typography>
      </Box>
    )
  }
  const Component = tab.component
  return (
    <Suspense
      fallback={
        <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
          <CircularProgress size={24} />
        </Box>
      }
    >
      <Component />
    </Suspense>
  )
}

export default BorderTabHost
