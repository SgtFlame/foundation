// mui.htmlView (forge 07l): untrusted HTML in a sandboxed frame. The frame
// is sandboxed without allow-scripts, allow-forms, allow-popups or
// navigation, so nothing in it runs, submits or opens; it is same-origin
// only so this page can size it to its content, fold its quotes and catch
// its link clicks (scripts stay off, so that grants the content nothing).
// Its Content-Security-Policy loads nothing but data: URLs and the
// tenant's record files until remote images are allowed.

import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Typography } from '@mui/material'
import type { RendererProps } from '../../points.ts'
import { foldQuotes, frameStyle, mismatched, policy, sanitize } from './htmlSanitize.ts'

const escapeAttr = (s: string) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;')

export function HtmlView(p: RendererProps) {
  const html = typeof p.props.html === 'string' ? p.props.html : ''
  const [ownAllow, setOwnAllow] = useState(false)
  const allowRemote = p.props.allowRemote === true || ownAllow
  const collapse = p.props.collapseQuotes !== false
  const maxHeight = p.props.maxHeight as number | undefined
  const frame = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState(40)
  const [pending, setPending] = useState<{ href: string; text: string }>()

  const { srcDoc, remoteImages } = useMemo(() => {
    const clean = sanitize(html, allowRemote)
    const doc =
      `<!doctype html><html><head><meta charset="utf-8">` +
      `<meta http-equiv="Content-Security-Policy" content="${escapeAttr(policy(location.origin, allowRemote))}">` +
      `<style>${frameStyle}</style></head><body>${clean.body}</body></html>`
    return { srcDoc: doc, remoteImages: clean.remoteImages }
  }, [html, allowRemote])

  const emit = useRef(p.emit)
  emit.current = p.emit

  // Once loaded: fold quotes, follow its size, catch link clicks
  useEffect(() => {
    const el = frame.current
    if (!el) return
    let observer: ResizeObserver | undefined
    const onLoad = () => {
      const doc = el.contentDocument
      if (!doc?.body) return
      if (collapse) foldQuotes(doc)
      const measure = () => setHeight(Math.ceil(doc.documentElement.scrollHeight))
      observer?.disconnect()
      observer = new ResizeObserver(measure)
      observer.observe(doc.body)
      doc.querySelectorAll('img').forEach((img) => img.addEventListener('load', measure))
      measure()
      doc.addEventListener('click', (e) => {
        const target = e.target as Element | null
        const toggle = target?.closest('.fq-toggle')
        if (toggle) {
          toggle.nextElementSibling?.classList.toggle('fq-hidden')
          measure()
          return
        }
        const a = target?.closest('a')
        if (!a) return
        e.preventDefault()
        const href = a.getAttribute('href') ?? ''
        if (!href || href.startsWith('#')) return
        const text = a.textContent ?? ''
        if (mismatched(text, href)) setPending({ href, text })
        else open(href, text, false)
      })
      // Nothing in the frame navigates: a form's Enter, a stray submit
      doc.addEventListener('submit', (e) => e.preventDefault(), true)
    }
    el.addEventListener('load', onLoad)
    return () => {
      el.removeEventListener('load', onLoad)
      observer?.disconnect()
    }
  }, [srcDoc, collapse])

  function open(href: string, text: string, warned: boolean) {
    window.open(href, '_blank', 'noopener,noreferrer')
    emit.current('link', { href, text, mismatched: warned })
  }

  const shown = maxHeight ? Math.min(height, maxHeight) : height
  return (
    <Box data-html-view>
      {remoteImages > 0 && !allowRemote && (
        <Alert severity="info" variant="outlined" sx={{ mb: 1, py: 0 }}
          action={<Button size="small" onClick={() => (p.handles('allowRemote') ? p.emit('allowRemote', {}) : setOwnAllow(true))}>Show images</Button>}>
          Remote images are hidden to protect your privacy.
        </Alert>
      )}
      <Box component="iframe" ref={frame} title="Message" sandbox="allow-same-origin" referrerPolicy="no-referrer" srcDoc={srcDoc}
        sx={{ display: 'block', width: '100%', height: shown, border: 0, bgcolor: '#fff', borderRadius: 1 }}
        scrolling={maxHeight && height > maxHeight ? 'yes' : 'no'} />
      <Dialog open={!!pending} onClose={() => setPending(undefined)} maxWidth="sm">
        <DialogTitle>This link goes somewhere else</DialogTitle>
        <DialogContent>
          <Typography variant="body2" gutterBottom>The link reads <b>{pending?.text}</b> but opens</Typography>
          <Typography variant="body2" sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>{pending?.href}</Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPending(undefined)}>Cancel</Button>
          <Button color="warning" onClick={() => { if (pending) open(pending.href, pending.text, true); setPending(undefined) }}>Open anyway</Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}

export default HtmlView
