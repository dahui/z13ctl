// Post-build check that every internal link's #fragment names an element on
// the page it points at. VitePress's dead-link check covers pages only, and
// a heading rename (or a slug rule that differs from the old site's — it
// turns ryzen_smu into ryzen-smu) otherwise leaves links that land at the
// top of the page with nothing said.
import { readdirSync, readFileSync } from 'node:fs'
import { join, relative, sep } from 'node:path'
import type { SiteConfig } from 'vitepress'

function htmlFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name)
    if (e.isDirectory()) return e.name === 'assets' ? [] : htmlFiles(p)
    return e.name.endsWith('.html') ? [p] : []
  })
}

// The URL a built page is served at, with cleanUrls: reference/daemon.html
// is /reference/daemon, gui/index.html is /gui/.
function urlOf(outDir: string, file: string): string {
  const rel = relative(outDir, file).split(sep).join('/')
  if (rel === 'index.html') return '/'
  if (rel.endsWith('/index.html')) return '/' + rel.slice(0, -'index.html'.length)
  return '/' + rel.slice(0, -'.html'.length)
}

// Only the page's own content: the sidebar, nav and outline repeat links
// that are checked where they are written.
function content(html: string): string {
  const start = html.indexOf('<main')
  const end = html.indexOf('</main>')
  return start >= 0 && end > start ? html.slice(start, end) : html
}

const decode = (s: string) => s.replace(/&amp;/g, '&').replace(/&quot;/g, '"')

export function checkAnchors(site: SiteConfig): void {
  const base = site.site.base // '/voltaire/'
  const pages = new Map<string, { ids: Set<string>; html: string }>()
  for (const file of htmlFiles(site.outDir)) {
    if (file.endsWith('404.html')) continue
    const html = readFileSync(file, 'utf8')
    const ids = new Set<string>()
    for (const m of html.matchAll(/\s(?:id|name)="([^"]+)"/g)) ids.add(decode(m[1]))
    pages.set(urlOf(site.outDir, file), { ids, html: content(html) })
  }

  const problems: string[] = []
  for (const [url, page] of pages) {
    for (const m of page.html.matchAll(/href="([^"]*#[^"]+)"/g)) {
      const href = decode(m[1])
      let path: string
      if (href.startsWith('#')) path = url
      else if (href.startsWith(base)) path = '/' + href.slice(base.length).split('#')[0]
      else continue // external
      const frag = decodeURIComponent(href.slice(href.indexOf('#') + 1))
      const target = pages.get(path) ?? pages.get(path.replace(/\.html$/, ''))
      if (!target) problems.push(`${url}: ${href} → no such page`)
      else if (!target.ids.has(frag)) problems.push(`${url}: ${href} → no #${frag} on ${path}`)
    }
  }
  if (problems.length > 0) {
    throw new Error(`broken anchor links:\n  ${problems.join('\n  ')}`)
  }
}
