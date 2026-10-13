/**
 * Typed HTTP access to the /v1 API.
 *
 * Errors carry the status so callers can tell a real 401 (sign in) apart from a
 * 500, a 429 or a dropped connection. A bare Error cannot: `statusText` is also
 * empty under HTTP/2, so the message could be '' and render as nothing at all.
 */

/** An error response from the server. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }

  /** True when the session is gone and the user must sign in again. */
  get isUnauthorized(): boolean {
    return this.status === 401
  }

  /** True when the caller is being rate limited. */
  get isRateLimited(): boolean {
    return this.status === 429
  }
}

/** The request never produced a response: offline, DNS failure, or a timeout. */
export class NetworkError extends Error {
  readonly cause?: unknown

  constructor(message: string, cause?: unknown) {
    super(message)
    this.name = 'NetworkError'
    this.cause = cause
  }
}

export const DEFAULT_TIMEOUT_MS = 15_000

type RequestOptions = {
  timeoutMs?: number
}

type ErrorBody = { error?: { code?: string; message?: string } }

async function toResult(r: Response): Promise<unknown> {
  // A body that is not JSON (a proxy error page, an empty 502) must not throw
  // past this point, or the real status is lost.
  const data = (await r.json().catch(() => ({}))) as ErrorBody
  if (!r.ok) {
    const code = data?.error?.code ?? `http_${r.status}`
    // statusText is empty under HTTP/2, so fall back rather than rendering "".
    const message = data?.error?.message || r.statusText || `Request failed (${r.status})`
    throw new ApiError(r.status, code, message)
  }
  return data
}

/**
 * `any` is deliberate at this boundary: the response DTOs are not generated
 * from the Go types yet (see #94), and every caller reads ad-hoc fields off
 * the result. Errors, which callers actually branch on, are fully typed.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function request(path: string, init: RequestInit, opts: RequestOptions = {}): Promise<any> {
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)

  try {
    const r = await fetch(path, { ...init, signal: controller.signal })
    return await toResult(r)
  } catch (err) {
    if (err instanceof ApiError) throw err
    // An abort here is our timeout firing, not the caller cancelling.
    if (controller.signal.aborted) {
      throw new NetworkError(`Request to ${path} timed out after ${timeoutMs}ms`, err)
    }
    throw new NetworkError(`Could not reach the server: ${(err as Error)?.message ?? 'network error'}`, err)
  } finally {
    clearTimeout(timer)
  }
}

const headers = { Accept: 'application/json' }

const jsonInit = (method: string, body?: unknown): RequestInit => ({
  method,
  credentials: 'include',
  headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
  body: body === undefined ? undefined : JSON.stringify(body),
})

export const api = {
  get: (path: string, opts?: RequestOptions): Promise<any> =>
    request(path, { credentials: 'include', headers }, opts),
  del: (path: string, opts?: RequestOptions): Promise<any> =>
    request(path, { method: 'DELETE', credentials: 'include', headers }, opts),
  post: (path: string, body?: unknown, opts?: RequestOptions): Promise<any> =>
    request(path, jsonInit('POST', body), opts),
  patch: (path: string, body?: unknown, opts?: RequestOptions): Promise<any> =>
    request(path, jsonInit('PATCH', body), opts),
  upload: (path: string, file: File, opts?: RequestOptions): Promise<any> => {
    const fd = new FormData()
    fd.append('photo', file)
    // No Content-Type here: the browser must set the multipart boundary itself.
    return request(path, { method: 'POST', credentials: 'include', body: fd }, opts)
  },
}

export type Me = {
  id: string
  name: string | null
  onboardingStep: string
  status: string
  role?: string
  xp: number
  level: number
  incognito: boolean
  metroId: string | null
  photos: { id: string; url: string; state: string }[]
  intents: string[]
}