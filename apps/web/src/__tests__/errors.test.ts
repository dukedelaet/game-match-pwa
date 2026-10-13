import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { installGlobalErrorReporting, toMessage } from '../errors'

describe('global error reporting', () => {
  let reported: unknown[]
  let handler: (e: unknown) => void
  let errHandler: (e: unknown) => void

  let teardown: () => void

  beforeEach(() => {
    reported = []
    // Without tearing down, each test adds another listener and one dispatch
    // reports N times.
    teardown = installGlobalErrorReporting((e: unknown) => reported.push(e))
    handler = (e) =>
      window.dispatchEvent(
        new PromiseRejectionEvent('unhandledrejection', {
          promise: Promise.resolve(),
          reason: e,
        }),
      )
    errHandler = (e) => window.dispatchEvent(new ErrorEvent('error', { error: e as Error, message: String(e) }))
  })
  afterEach(() => {
    teardown?.()
  })

  it('reports an unhandled promise rejection', () => {
    handler(new Error('boom'))
    expect(reported).toHaveLength(1)
  })

  it('reports a window error', () => {
    errHandler(new Error('kaboom'))
    expect(reported).toHaveLength(1)
  })

  // React 19 does not catch rejections from async event handlers, so a failed
  // tap produced no user-visible feedback at all: the button simply did
  // nothing and the error only reached the console.
  it('turns an unknown throwable into something displayable', () => {
    expect(toMessage(new Error('real problem'))).toBe('real problem')
    expect(toMessage('a string')).toBe('a string')
    expect(toMessage({ weird: true })).toBe('Something went wrong')
    expect(toMessage(undefined)).toBe('Something went wrong')
  })

  // The same failure arrives as both an error and a rejection event; showing
  // the user the same message twice is worse than once.
  it('does not report the same failure twice', () => {
    const boom = new Error('once')
    handler(boom)
    errHandler(boom)
    expect(reported).toHaveLength(1)
  })
})
