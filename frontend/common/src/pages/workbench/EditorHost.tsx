import { Box, CircularProgress, Typography } from '@mui/material'
import { Suspense } from 'react'
import { useContributions } from '../../workbench/extensions/index.ts'
import { editors, type EditableObject } from '../../workbench/points.ts'
import type { EditorRegistry } from './EditorRegistry.ts'

// Renders the workbench.editor contribution for a tab's object
function EditorHost({ object, registry, tabId }: { object: EditableObject; registry: EditorRegistry; tabId: string }) {
  const editor = useContributions(editors).find((c) => c.id === object.kind)
  if (!editor) {
    return (
      <Box sx={{ p: 2 }}>
        <Typography color="text.secondary">No editor is available for {object.kind}.</Typography>
      </Box>
    )
  }
  const Component = editor.component
  return (
    <Suspense
      fallback={
        <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
          <CircularProgress size={24} />
        </Box>
      }
    >
      <Component object={object as never} registry={registry} tabId={tabId} />
    </Suspense>
  )
}

export default EditorHost
