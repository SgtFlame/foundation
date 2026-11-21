// Sanitizing untrusted HTML for mui.htmlView (forge 07l). The HTML is
// parsed inert (DOMParser runs nothing and loads nothing), cleaned, and
// written into a sandboxed frame whose Content-Security-Policy loads
// nothing from the network but data: URLs and the tenant's record files,
// until remote images are allowed. Sanitizing is the first wall and the
// policy the second: either alone keeps a hostile message from running a
// script, submitting a form or calling home.

const dropped = [
  'script', 'noscript', 'iframe', 'frame', 'frameset', 'object', 'embed', 'applet', 'meta', 'base', 'link', 'template',
  'portal', 'video', 'audio', 'source', 'track', 'canvas', 'dialog', 'input', 'button', 'select', 'textarea', 'option',
  'math', 'title',
]
const urlAttrs = ['href', 'src', 'action', 'formaction', 'xlink:href', 'srcset', 'poster', 'ping', 'background', 'data', 'codebase', 'cite', 'lowsrc', 'dynsrc']
const remote = /^(https?:)?\/\//i
// A CSS url() of the network; until remote images are allowed it becomes
// none, so the frame does not even try (the policy would block it)
const remoteUrl = /url\(\s*(['"]?)\s*(https?:)?\/\/[^)]*\)/gi
// Fonts never load from the network, allowed or not
const fontFace = /@font-face\s*\{[^}]*\}/gi
const safeLink = /^(https?:|mailto:|tel:|#)/i

export interface Sanitized {
  body: string // the body's HTML, with the head's styles
  remoteImages: number // images (and backgrounds) waiting for allowRemote
}

// sanitize cleans html; allowRemote keeps remote images' src (data-src
// becomes src), else every remote image waits in data-src.
export function sanitize(html: string, allowRemote: boolean): Sanitized {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const styles = [...doc.querySelectorAll('style')].map((s) => s.textContent ?? '')
  doc.querySelectorAll('style').forEach((s) => s.remove())
  doc.querySelectorAll(dropped.join(',')).forEach((el) => el.remove())
  // A form shows its contents but submits nothing
  doc.querySelectorAll('form').forEach((form) => {
    const div = doc.createElement('div')
    div.append(...form.childNodes)
    form.replaceWith(div)
  })
  doc.querySelectorAll('svg script, svg foreignObject, svg use, svg animate, svg set').forEach((el) => el.remove())
  let remoteImages = 0
  for (const el of doc.body.querySelectorAll('*')) {
    for (const attr of [...el.attributes]) {
      const name = attr.name.toLowerCase()
      if (name.startsWith('on') || name === 'formaction' || name === 'srcdoc' || name === 'http-equiv') el.removeAttribute(attr.name)
    }
    // Images: remote ones wait in data-src until allowed
    if (el.tagName === 'IMG') {
      const src = el.getAttribute('src') ?? ''
      const waiting = el.getAttribute('data-src') ?? ''
      el.removeAttribute('srcset')
      if (remote.test(src) || waiting) {
        const url = remote.test(src) ? src : waiting
        if (allowRemote && remote.test(url)) {
          el.setAttribute('src', url)
          el.removeAttribute('data-src')
        } else {
          el.removeAttribute('src')
          el.setAttribute('data-src', url)
          remoteImages++
        }
      } else if (src && !/^data:image\//i.test(src) && !src.startsWith('/api/v1/records/files/')) {
        el.removeAttribute('src')
      }
    }
    // Legacy backgrounds: shown as a CSS background once remote images are
    for (const name of ['background', 'data-background']) {
      const v = el.getAttribute(name)
      if (!v) continue
      el.removeAttribute(name)
      if (remote.test(v)) {
        if (allowRemote) (el as HTMLElement).style.backgroundImage = `url("${v.replace(/["\\]/g, '')}")`
        else remoteImages++
      }
    }
    // Links keep http, mailto and tel targets only
    if (el.tagName === 'A' || el.tagName === 'AREA') {
      const href = (el.getAttribute('href') ?? '').trim()
      if (!safeLink.test(href)) el.removeAttribute('href')
    }
    for (const name of urlAttrs) {
      if (name === 'href' && (el.tagName === 'A' || el.tagName === 'AREA')) continue
      if (name === 'src' && el.tagName === 'IMG') continue
      el.removeAttribute(name)
    }
    // Inline styles: no positioning over the page, no expressions, no
    // bindings; their url()s are left to the frame's policy
    let style = el.getAttribute('style')
    remoteUrl.lastIndex = 0
    if (style && !allowRemote && remoteUrl.test(style)) {
      remoteImages++
      style = style.replace(remoteUrl, 'none')
      el.setAttribute('style', style)
    }
    remoteUrl.lastIndex = 0
    if (style && /expression\s*\(|behavior\s*:|-moz-binding|javascript:/i.test(style)) el.removeAttribute('style')
    else if (style && /position\s*:\s*(fixed|sticky)/i.test(style)) (el as HTMLElement).style.position = 'static'
  }
  let css = styles
    .join('\n')
    .replace(/@import[^;]*;?/gi, '')
    .replace(fontFace, '')
    .replace(/expression\s*\(|behavior\s*:|-moz-binding|javascript:/gi, 'x-blocked:')
    .replace(/position\s*:\s*(fixed|sticky)/gi, 'position: static')
  const remoteCss = (css.match(remoteUrl) ?? []).length
  if (!allowRemote) css = css.replace(remoteUrl, 'none')
  return { body: (css ? `<style>${css}</style>` : '') + doc.body.innerHTML, remoteImages: remoteImages + remoteCss }
}

// policy is the frame's Content-Security-Policy: nothing from the network
// but data: and the tenant's files, and remote images when allowed.
export function policy(origin: string, allowRemote: boolean): string {
  const images = `data: ${origin}/api/v1/records/files/${allowRemote ? ' https: http:' : ''}`
  return [
    "default-src 'none'",
    `img-src ${images}`,
    "style-src 'unsafe-inline'",
    'font-src data:',
    "form-action 'none'",
    "base-uri 'none'",
    "frame-src 'none'",
    "media-src 'none'",
    "object-src 'none'",
    "script-src 'none'",
  ].join('; ')
}

// quoteRoots are the quoted parts of a message: blockquotes not inside
// another, Gmail's quote and Outlook's reply header and what follows it.
export function foldQuotes(doc: Document): number {
  const roots: Element[] = []
  // Outlook: the reply header and everything after it in its parent
  const outlook = doc.querySelector('#divRplyFwdMsg, #appendonsend, div[id^="x_divRplyFwdMsg"]')
  if (outlook?.parentElement) {
    const wrap = doc.createElement('div')
    outlook.parentElement.insertBefore(wrap, outlook)
    let n: ChildNode | null = outlook
    while (n) {
      const next: ChildNode | null = n.nextSibling
      wrap.append(n)
      n = next
    }
    roots.push(wrap)
  }
  for (const q of doc.querySelectorAll('.gmail_quote, blockquote')) {
    if (roots.some((r) => r.contains(q))) continue
    if (q.parentElement?.closest('.gmail_quote, blockquote')) continue
    roots.push(q)
  }
  for (const root of roots) {
    const toggle = doc.createElement('div')
    toggle.className = 'fq-toggle'
    toggle.setAttribute('title', 'Show quoted text')
    toggle.textContent = '…'
    root.parentElement?.insertBefore(toggle, root)
    root.classList.add('fq-hidden')
  }
  return roots.length
}

// The frame's own styles: a page of mail, its quote toggles
export const frameStyle = `
html { color-scheme: light; }
body { margin: 8px; font-family: system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif; font-size: 14px; line-height: 1.45; color: #202124; background: #fff; overflow-wrap: anywhere; }
img { max-width: 100%; height: auto; }
img[data-src] { display: inline-block; min-width: 1px; min-height: 1px; background: #eee; }
pre { white-space: pre-wrap; }
.fq-hidden { display: none !important; }
.fq-toggle { display: inline-block; cursor: pointer; padding: 0 8px; margin: 4px 0; border-radius: 8px; background: #e8eaed; color: #5f6368; font-weight: bold; letter-spacing: 1px; user-select: none; }
.fq-toggle:hover { background: #dadce0; }
`

// mismatched reports whether a link's text names a site other than the one
// it goes to (the text reads as a URL or a domain).
export function mismatched(text: string, href: string): boolean {
  const t = text.trim().toLowerCase()
  const m = t.match(/^(?:https?:\/\/)?(?:www\.)?([a-z0-9-]+(?:\.[a-z0-9-]+)+)(?:[/:?#]|$)/)
  if (!m) return false
  let host: string
  try {
    host = new URL(href).hostname.toLowerCase().replace(/^www\./, '')
  } catch {
    return false
  }
  const named = m[1]
  return named !== host && !host.endsWith('.' + named) && !named.endsWith('.' + host)
}
