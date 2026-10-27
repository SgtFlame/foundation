import { useState, type ReactNode } from 'react'
import { Box, Tab, Tabs, Typography } from '@mui/material'
import type { NetworkEntry } from '../../../services/http/NetworkStore.ts'
import JsonView from '../../../components/json/JsonView.tsx'
import { formatSize } from './format.ts'

type DetailTab = 'headers' | 'payload' | 'preview' | 'response' | 'timing'

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Box sx={{ mb: 1.5 }}>
      <Typography variant="subtitle2" sx={{ fontSize: 12, borderBottom: 1, borderColor: 'divider', mb: 0.5 }}>
        {title}
      </Typography>
      {children}
    </Box>
  )
}

function KeyValues({ values }: { values: Record<string, unknown> }) {
  const entries = Object.entries(values)
  if (entries.length === 0) {
    return <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>None</Typography>
  }
  return (
    <Box
      component="dl"
      sx={{
        m: 0,
        display: 'grid',
        gridTemplateColumns: 'max-content 1fr',
        columnGap: 1.5,
        fontSize: 12,
        fontFamily: 'monospace',
        '& dt': { color: 'text.secondary' },
        '& dd': { m: 0, wordBreak: 'break-all' },
      }}
    >
      {entries.map(([key, value]) => (
        <Box key={key} sx={{ display: 'contents' }}>
          <dt>{key}</dt>
          <dd>{String(value ?? '')}</dd>
        </Box>
      ))}
    </Box>
  )
}

function statusText(entry: NetworkEntry): string {
  if (entry.state === 'pending') return '(pending)'
  if (entry.state === 'failed') return `(failed) ${entry.error ?? ''}`
  return `${entry.status} ${entry.statusText ?? ''}`
}

// Headers / Payload / Preview / Response / Timing for one captured request
function RequestDetails({ entry }: { entry: NetworkEntry }) {
  const [tab, setTab] = useState<DetailTab>('headers')

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      <Tabs
        value={tab}
        onChange={(_, next: DetailTab) => setTab(next)}
        variant="scrollable"
        sx={{ minHeight: 32, borderBottom: 1, borderColor: 'divider', '& .MuiTab-root': { minHeight: 32, py: 0, fontSize: 12, textTransform: 'none' } }}
      >
        <Tab value="headers" label="Headers" />
        <Tab value="payload" label="Payload" />
        <Tab value="preview" label="Preview" />
        <Tab value="response" label="Response" />
        <Tab value="timing" label="Timing" />
      </Tabs>
      <Box sx={{ flexGrow: 1, overflow: 'auto', p: 1 }}>
        {tab === 'headers' && (
          <>
            <Section title="General">
              <KeyValues
                values={{
                  'Request URL': entry.url,
                  'Request Method': entry.method,
                  'Status Code': statusText(entry),
                }}
              />
            </Section>
            <Section title="Response Headers">
              <KeyValues values={entry.responseHeaders ?? {}} />
            </Section>
            <Section title="Request Headers">
              <KeyValues values={entry.requestHeaders} />
            </Section>
          </>
        )}
        {tab === 'payload' && <JsonView key={`${entry.id}-payload`} value={entry.requestBody} />}
        {tab === 'preview' && <JsonView key={`${entry.id}-preview`} value={entry.responseBody} />}
        {tab === 'response' && (
          <JsonView key={`${entry.id}-response`} value={entry.responseBody} defaultMode="raw" />
        )}
        {tab === 'timing' && (
          <KeyValues
            values={{
              Started: new Date(entry.startedAt).toISOString(),
              Ended: entry.endedAt ? new Date(entry.endedAt).toISOString() : '(pending)',
              Duration: entry.durationMs !== undefined ? `${entry.durationMs} ms` : '(pending)',
              Size: formatSize(entry.sizeBytes),
            }}
          />
        )}
      </Box>
    </Box>
  )
}

export default RequestDetails
