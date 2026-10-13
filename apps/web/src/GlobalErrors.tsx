import { useEffect, useState } from 'react'
import { installGlobalErrorReporting, toMessage } from './errors'

/**
 * Backstop for failures React cannot catch.
 *
 * React 19 does not catch rejections from async event handlers, so a failing
 * tap reached only the console and the user saw a button that did nothing.
 * This surfaces it in a toast instead.
 */
export function GlobalErrors({ children }: { children: React.ReactNode }) {
  const [message, setMessage] = useState('')

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const show = (err: unknown) => {
      setMessage(toMessage(err))
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => setMessage(''), 6000)
    }
    const teardown = installGlobalErrorReporting(show)
    return () => {
      if (timer) clearTimeout(timer)
      teardown()
    }
  }, [])

  return (
    <>
      {children}
      {message && (
        <div
          role="alert"
          className="fixed inset-x-4 bottom-4 z-50 rounded-2xl bg-rose-950/95 px-4 py-3 text-sm text-rose-100 shadow-lg"
        >
          {message}
        </div>
      )}
    </>
  )
}
