import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { api, ApiError, NetworkError } from '../api'

// rejected returns the rejection reason typed, so assertions can read .status
// and .message without an `any` escape hatch.
async function rejected(promise: Promise<unknown>): Promise<any> {
  return promise.then(
    () => {
      throw new Error('expected the request to reject')
    },
    (e) => e,
  )
}

function mockResponse(body: unknown, init: ResponseInit = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
}

describe('api error handling', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('exposes the HTTP status on API errors', async () => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse({ error: { code: 'unauth', message: 'Sign in' } }, { status: 401 }),
    )

    const err = await rejected(api.get('/v1/auth/session'))
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(401)
    expect(err.code).toBe('unauth')
  })

  it.each([403, 404, 409, 429, 500])('distinguishes status %i from every other', async (status) => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse({ error: { code: 'x', message: 'nope' } }, { status }),
    )

    const err = await rejected(api.get('/v1/anything'))
    expect(err.status).toBe(status)
  })

  // statusText is empty under HTTP/2, so a bare Error could carry an empty
  // message and the UI would render nothing at all.
  it('never produces an empty error message', async () => {
    const res = new Response(JSON.stringify({}), { status: 500, statusText: '' })
    vi.mocked(fetch).mockResolvedValue(res)

    const err = await rejected(api.get('/v1/anything'))
    expect(err.message.length).toBeGreaterThan(0)
  })

  it('reports a network failure distinctly from an API error', async () => {
    vi.mocked(fetch).mockRejectedValue(new TypeError('Failed to fetch'))

    const err = await rejected(api.get('/v1/anything'))
    expect(err).toBeInstanceOf(NetworkError)
    expect(err).not.toBeInstanceOf(ApiError)
  })

  it('sends the session cookie on every method', async () => {
    vi.mocked(fetch).mockResolvedValue(mockResponse({ ok: true }))

    await api.get('/v1/a')
    await api.post('/v1/a', {})
    await api.patch('/v1/a', {})
    await api.del('/v1/a')

    for (const call of vi.mocked(fetch).mock.calls) {
      expect(call[1]?.credentials).toBe('include')
    }
  })

  it('aborts a request that exceeds the timeout', async () => {
    vi.mocked(fetch).mockImplementation(
      (_url, init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
        }),
    )

    const err = await rejected(api.get('/v1/slow', { timeoutMs: 10 }))
    expect(err).toBeInstanceOf(NetworkError)
    expect(err.message).toMatch(/timed out|cancel/i)
  })
})