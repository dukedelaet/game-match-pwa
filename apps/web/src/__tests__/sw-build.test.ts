import { describe, expect, it } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'

// vite-plugin-pwa emits `createHandlerBoundToURL(url)` for navigateFallback at the
// top level of the generated worker. Workbox resolves that against the precache
// manifest at *evaluation* time and throws `non-precached-url` on a miss, so a
// miss does not degrade the worker, it stops the whole script -- and with it
// register(), precaching, the offline fallback and the push listeners.
//
// These tests read the built worker, because that is the artifact browsers run.
// Asserting on vite.config.ts instead would not catch a plugin that changed how
// it resolves the URL.

// process.cwd() is the package root under vitest, in both the node and jsdom
// environments; fileURLToPath on import.meta.url is not reliable under jsdom.
const distDir = resolve(process.cwd(), 'dist')
const swPath = resolve(distDir, 'sw.js')
const built = existsSync(swPath)

/** URLs workbox will precache, as listed in the built worker. */
function precachedUrls(source: string): string[] {
  const block = source.match(/precacheAndRoute\(\[([^\]]*)\]/)
  if (!block) throw new Error('no precacheAndRoute call found in sw.js')
  return [...block[1].matchAll(/url:"([^"]+)"/g)].map((m) => m[1])
}

/** Every URL passed to createHandlerBoundToURL, which must resolve from the cache. */
function fallbackUrls(source: string): string[] {
  return [...source.matchAll(/createHandlerBoundToURL\("([^"]+)"\)/g)].map((m) => m[1])
}

describe.skipIf(!built)('generated service worker', () => {
  const source = readFileSync(swPath, 'utf8')

  it('binds every navigation fallback to a precached URL', () => {
    const precached = precachedUrls(source)
    const fallbacks = fallbackUrls(source)

    expect(fallbacks.length).toBeGreaterThan(0)
    // "/offline" is a client-side route, not a build output, so it is never in
    // the manifest and workbox throws when the worker is evaluated.
    for (const url of fallbacks) {
      // The manifest stores bare paths; workbox resolves fallbacks as absolute URLs.
      const bare = url.replace(/^\//, '')
      expect(precached, `"${url}" must be precached or the worker fails to install`).toContain(bare)
    }
  })

  it('precaches the app shell', () => {
    expect(precachedUrls(source)).toContain('index.html')
  })

  // autoUpdate emitted skipWaiting() + clientsClaim(), swapping the worker under
  // a live game. The update must wait for the user.
  it('holds the new worker instead of swapping it under open tabs', () => {
    expect(source).toContain('skipWaiting')
    expect(source, 'autoUpdate implies skipWaiting(); prompt mode must not').not.toContain('clientsClaim()')
  })

  it('ships the icons an install prompt needs', () => {
    const manifest = JSON.parse(readFileSync(resolve(distDir, 'manifest.webmanifest'), 'utf8'))
    const pngs = manifest.icons.filter((i: { type: string }) => i.type === 'image/png')
    const sizes = pngs.map((i: { sizes: string }) => i.sizes)

    expect(sizes).toContain('192x192')
    expect(sizes).toContain('512x512')
    expect(pngs.some((i: { purpose: string }) => i.purpose === 'maskable')).toBe(true)

    // The files must actually exist, or the install prompt offers a broken icon.
    for (const icon of pngs) {
      expect(existsSync(resolve(distDir, icon.src.replace(/^\//, '')))).toBe(true)
    }
  })

  it('ships an apple-touch-icon so iOS does not screenshot the page', () => {
    const html = readFileSync(resolve(distDir, 'index.html'), 'utf8')
    expect(html).toContain('apple-touch-icon')
  })

  it('keeps the push worker alongside the generated worker', () => {
    expect(source).toContain('importScripts("push-sw.js")')
    expect(precachedUrls(source)).toContain('push-sw.js')
  })
})