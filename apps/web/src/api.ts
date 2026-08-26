const j = async (r: Response) => {
  const data = await r.json().catch(() => ({}))
  if (!r.ok) {
    const msg = data?.error?.message || r.statusText
    throw new Error(msg)
  }
  return data
}

const headers = { Accept: 'application/json' }

export const api = {
  get: (path: string) => fetch(path, { credentials: 'include', headers }).then(j),
  del: (path: string) => fetch(path, { method: 'DELETE', credentials: 'include', headers }).then(j),
  post: (path: string, body?: unknown) =>
    fetch(path, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    }).then(j),
  patch: (path: string, body: unknown) =>
    fetch(path, {
      method: 'PATCH',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(body),
    }).then(j),
  upload: (path: string, file: File) => {
    const fd = new FormData()
    fd.append('photo', file)
    return fetch(path, { method: 'POST', credentials: 'include', body: fd }).then(j)
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
