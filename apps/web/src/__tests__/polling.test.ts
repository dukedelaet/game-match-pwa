import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { startPolling } from '../polling'
import { ApiError } from '../api'

describe('startPolling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('polls on an interval rather than only on mount', async () => {
    const fetch = vi.fn().mockResolvedValue({ state: 'in_round' })
    const stop = startPolling({ fetch, onData: vi.fn(), intervalMs: 3000 })

    await vi.advanceTimersByTimeAsync(0)
    expect(fetch).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(3000)
    expect(fetch).toHaveBeenCalledTimes(2)

    stop()
  })

  // The original bug: a request slower than the interval overlapped the next
  // tick, so a client on a slow connection issued more requests than the
  // 2-per-second server limit allows.
  it('never runs two requests at once, however slow a response is', async () => {
    let concurrent = 0
    let maxConcurrent = 0
    let resolveNext: (() => void) | undefined

    const fetch = vi.fn(() => {
      concurrent++
      maxConcurrent = Math.max(maxConcurrent, concurrent)
      return new Promise<void>((resolve) => {
        resolveNext = () => {
          concurrent--
          resolve()
        }
      })
    })

    const stop = startPolling({ fetch, onData: vi.fn(), intervalMs: 1000 })

    // Five intervals pass while the first request is still outstanding.
    await vi.advanceTimersByTimeAsync(5000)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(maxConcurrent).toBe(1)

    resolveNext?.()
    await vi.advanceTimersByTimeAsync(3000)
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(maxConcurrent).toBe(1)

    stop()
  })

  it('slows down when rate limited instead of hammering', async () => {
    const fetch = vi.fn().mockRejectedValue(new ApiError(429, 'rate', 'slow down'))
    const onError = vi.fn()
    const stop = startPolling({ fetch, onData: vi.fn(), onError, intervalMs: 3000 })

    await vi.advanceTimersByTimeAsync(0)
    expect(onError).toHaveBeenCalledTimes(1)

    // First backoff step is 6s, not the 3s base.
    await vi.advanceTimersByTimeAsync(3000)
    expect(fetch).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(3000)
    expect(fetch).toHaveBeenCalledTimes(2)

    stop()
  })

  it('backs off further on repeated failures and caps the interval', async () => {
    const fetch = vi.fn().mockRejectedValue(new Error('down'))
    const stop = startPolling({ fetch, onData: vi.fn(), onError: vi.fn(), intervalMs: 1000, maxIntervalMs: 8000 })

    // 16s of virtual time at a doubling backoff starting from 1s: the client
    // makes roughly five attempts, not sixteen.
    await vi.advanceTimersByTimeAsync(16_000)
    expect(fetch.mock.calls.length).toBeLessThanOrEqual(6)
    expect(fetch.mock.calls.length).toBeGreaterThan(2)

    stop()
  })

  it('resets the backoff after a success', async () => {
    let fail = true
    const fetch = vi.fn(() => (fail ? Promise.reject(new Error('down')) : Promise.resolve({ ok: true })))
    const stop = startPolling({ fetch, onData: vi.fn(), onError: vi.fn(), intervalMs: 1000 })

    await vi.advanceTimersByTimeAsync(0) // fails
    await vi.advanceTimersByTimeAsync(2000) // fails again, backoff now 4s
    fail = false
    await vi.advanceTimersByTimeAsync(4000) // succeeds, interval back to base
    await vi.advanceTimersByTimeAsync(1000) // polls on the base interval again
    expect(fetch).toHaveBeenCalledTimes(4)

    stop()
  })

  it('stops entirely once disposed', async () => {
    const fetch = vi.fn().mockResolvedValue({})
    const onData = vi.fn()
    const stop = startPolling({ fetch, onData, intervalMs: 1000 })

    await vi.advanceTimersByTimeAsync(0)
    stop()
    await vi.advanceTimersByTimeAsync(10_000)

    expect(fetch).toHaveBeenCalledTimes(1)
  })

  // A response that lands after unmount must not set state on a dead component.
  it('ignores a response that arrives after being stopped', async () => {
    let resolveNext: ((v: unknown) => void) | undefined
    const fetch = vi.fn(() => new Promise((resolve) => { resolveNext = resolve }))
    const onData = vi.fn()

    const stop = startPolling({ fetch, onData, intervalMs: 1000 })
    await vi.advanceTimersByTimeAsync(0)
    stop()
    resolveNext?.({ state: 'forfeit' })
    await vi.advanceTimersByTimeAsync(0)

    expect(onData).not.toHaveBeenCalled()
  })

  it('defaults to an interval that fits the server rate limit', async () => {
    const fetch = vi.fn().mockResolvedValue({})
    const stop = startPolling({ fetch, onData: vi.fn() })

    // One poll on mount, the next at the 3s default.
    await vi.advanceTimersByTimeAsync(1000)
    expect(fetch).toHaveBeenCalledTimes(1)
    // Comfortably under the 2-per-rolling-second server cap even with a second
    // tab open on the same session.
    await vi.advanceTimersByTimeAsync(2000)
    expect(fetch).toHaveBeenCalledTimes(2)

    stop()
  })
})