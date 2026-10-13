import { describe, expect, it, afterEach, vi } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * The server is correctly guarded: it only returns the OTP in the response body
 * and only uses the constant code when APP_DEBUG is set. But the frontend
 * shipped the demo phone numbers and the OTP to every production visitor in the
 * built bundle, and prefilled the phone input with one of them.
 *
 * These assertions read the built JavaScript, because that is what a browser
 * downloads. Asserting on the source alone would miss a literal that gets
 * inlined by the minifier.
 */

const distDir = resolve(process.cwd(), 'dist')
const assetsDir = resolve(distDir, 'assets')

function builtScripts(): string[] {
  try {
    return readdirSync(assetsDir)
      .filter((f) => f.endsWith('.js'))
      .map((f) => readFileSync(resolve(assetsDir, f), 'utf8'))
  } catch {
    return []
  }
}

const built = builtScripts().length > 0

// Values that must never reach a production bundle.
const SECRETS = [
  '+15551111111',
  '+15552222222',
  '+15550000000',
  '123456',
]

describe.skipIf(!built)('production bundle', () => {
  const scripts = builtScripts().join('\n')

  it.each(SECRETS)('does not contain the demo credential %s', (secret) => {
    expect(scripts).not.toContain(secret)
  })

  it('does not ship the demo instructions copy', () => {
    expect(scripts).not.toMatch(/Demo:.*Jordan/i)
  })
})

describe('demo affordances are development-only', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.resetModules()
  })

  it('reports the demo phone only in dev', async () => {
    vi.stubEnv('DEV', true)
    vi.resetModules()
    const dev = await import('../demo')
    expect(dev.DEMO_PHONE).toBe('+15551111111')
  })

  it('reports no demo phone in production', async () => {
    vi.stubEnv('DEV', false)
    vi.resetModules()
    const prod = await import('../demo')
    expect(prod.DEMO_PHONE).toBe('')
    expect(prod.DEMO_HINT).toBe('')
  })
})