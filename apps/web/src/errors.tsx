/**
 * Global error capture.
 *
 * React 19 does not catch rejections from async event handlers, so every
 * failing button and every polling loop produced an unhandled rejection that
 * reached only the console. The user saw a button that did nothing, or a
 * screen frozen on stale data, with no message.
 *
 * These listeners are a backstop so a failure is at least reported. Handlers
 * that can render an inline error still should.
 */

/** Normalises any thrown value into text safe to show a user. */
export function toMessage(err: unknown, fallback = 'Something went wrong'): string {
  if (typeof err === 'string' && err.trim()) return err
  if (err instanceof Error && err.message) return err.message
  return fallback
}

/**
 * Installs listeners for unhandled rejections and window errors. Returns a
 * teardown function.
 */
export function installGlobalErrorReporting(report: (err: unknown) => void): () => void {
  // The same failure can surface as both an `error` and an `unhandledrejection`
  // event. Reporting it twice would show the user the same message twice, so
  // the last reported value is suppressed for one tick.
  let lastReported: unknown
  let lastAt = 0
  const DEDUPE_MS = 50

  const emit = (err: unknown) => {
    const now = Date.now()
    if (err === lastReported && now - lastAt < DEDUPE_MS) return
    lastReported = err
    lastAt = now
    report(err)
  }

  const onRejection = (event: PromiseRejectionEvent) => emit(event.reason)
  const onError = (event: ErrorEvent) => emit(event.error ?? event.message)

  window.addEventListener('unhandledrejection', onRejection)
  window.addEventListener('error', onError)
  return () => {
    window.removeEventListener('unhandledrejection', onRejection)
    window.removeEventListener('error', onError)
  }
}
