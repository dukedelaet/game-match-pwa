import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ApiError, NetworkError } from '../api'

/**
 * requireUser decides whether to bounce the user to the welcome screen. Before
 * the typed errors existed it caught everything, so a 500, a 429 or one dropped
 * connection was indistinguishable from a real 401 and logged the user out of
 * the UI -- mid-game if it happened on a navigation.
 *
 * The routing decision is reproduced here directly rather than through the
 * router, which is what the handler depends on.
 */
async function requireUserBehaviour(): Promise<'session' | 'redirect' | 'retry'> {
  const { api } = await import('../api')
  try {
    await api.get('/v1/auth/session')
    return 'session'
  } catch (err) {
    if (err instanceof ApiError && err.isUnauthorized) return 'redirect'
    if (err instanceof NetworkError) return 'retry'
    if (err instanceof ApiError) return 'retry'
    return 'redirect'
  }
}

describe('requireUser routing', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the session when the request succeeds', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ user: { id: 'u1' } }), { status: 200 }),
    )
    expect(await requireUserBehaviour()).toBe('session')
  })

  it('redirects to sign in on a genuine 401', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'unauth', message: 'Sign in' } }), { status: 401 }),
    )
    expect(await requireUserBehaviour()).toBe('redirect')
  })

  it('does not log the user out on a server error', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'server', message: 'boom' } }), { status: 500 }),
    )
    expect(await requireUserBehaviour()).toBe('retry')
  })

  it('does not log the user out when rate limited', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'rate', message: 'slow down' } }), { status: 429 }),
    )
    expect(await requireUserBehaviour()).toBe('retry')
  })

  it('does not log the user out on a dropped connection', async () => {
    vi.mocked(fetch).mockRejectedValue(new TypeError('Failed to fetch'))
    expect(await requireUserBehaviour()).toBe('retry')
  })
})