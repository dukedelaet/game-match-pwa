/**
 * Shared polling for the live session, queue and chat screens.
 *
 * Why this exists: the old code used `setInterval(poll, 1000)` against a server
 * limit of 2 requests per rolling second. Two tabs on the same session, or any
 * single request slower than a second, puts the client over the limit, and a 429
 * means the server never records the heartbeat -- so the 15s stale-poll backstop
 * forfeits the player mid-game with no message.
 *
 * Two properties matter and are tested:
 *   - never two requests in flight at once
 *   - back off on 429 so a rate-limited client stops making it worse
 */

/** A poller that calls onData with each fresh value and onError on failure. */
export type PollOptions<T> = {
  /** Performs one request. Should reject with an ApiError/NetworkError. */
  fetch: () => Promise<T>
  /** Receives each successful value. */
  onData: (value: T) => void
  /** Receives failures, e.g. to show "reconnecting". */
  onError?: (error: unknown) => void
  /** Base interval in ms. */
  intervalMs?: number
  /** Ceiling for the backoff in ms. */
  maxIntervalMs?: number
}

export function startPolling<T>(opts: PollOptions<T>): () => void {
  const base = opts.intervalMs ?? 3000
  const max = opts.maxIntervalMs ?? 15_000

  let stopped = false
  let timer: ReturnType<typeof setTimeout> | undefined
  let inFlight = false
  // Backoff grows on failure and resets on success, so one slow patch of
  // network does not leave the client polling slowly forever.
  let interval = base

  const schedule = (ms: number) => {
    if (stopped) return
    timer = setTimeout(run, ms)
  }

  const run = async () => {
    if (stopped || inFlight) return
    inFlight = true
    try {
      const value = await opts.fetch()
      if (stopped) return
      interval = base
      opts.onData(value)
    } catch (err) {
      if (stopped) return
      interval = Math.min(interval * 2, max)
      opts.onError?.(err)
    } finally {
      inFlight = false
      // The next request is scheduled after this one settles, so a slow
      // response can never overlap the next tick.
      schedule(interval)
    }
  }

  void run()

  return () => {
    stopped = true
    if (timer !== undefined) clearTimeout(timer)
  }
}