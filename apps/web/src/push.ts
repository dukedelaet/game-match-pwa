// Web Push opt-in. The server only accepts subscriptions when VAPID keys are
// configured, so this reports 'unavailable' rather than failing on a bare host.

export type PushResult = 'enabled' | 'unsupported' | 'denied' | 'unavailable'

const urlBase64ToUint8Array = (base64: string): Uint8Array<ArrayBuffer> => {
  const padding = '='.repeat((4 - (base64.length % 4)) % 4)
  const normalized = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(normalized)
  const bytes = new Uint8Array(new ArrayBuffer(raw.length))
  for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i)
  return bytes
}

export async function enablePush(): Promise<PushResult> {
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
    return 'unsupported'
  }

  const keyResponse = await fetch('/v1/push/vapid-public-key', {
    credentials: 'include',
    headers: { Accept: 'application/json' },
  })
  if (!keyResponse.ok) return 'unavailable'
  const { publicKey, enabled } = (await keyResponse.json()) as { publicKey?: string; enabled?: boolean }
  if (!enabled || !publicKey) return 'unavailable'

  const permission = await Notification.requestPermission()
  if (permission !== 'granted') return 'denied'

  const registration = await navigator.serviceWorker.ready
  const existing = await registration.pushManager.getSubscription()
  const subscription =
    existing ??
    (await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(publicKey),
    }))

  const json = subscription.toJSON()
  const response = await fetch('/v1/me/push', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ endpoint: json.endpoint, keys: json.keys }),
  })
  return response.ok ? 'enabled' : 'unavailable'
}

const messages: Record<PushResult, string> = {
  enabled: 'Notifications are on.',
  denied: 'Notifications are blocked in your browser settings.',
  unavailable: 'Notifications are not configured on this server yet.',
  unsupported: 'This browser does not support notifications.',
}

export const pushResultMessage = (result: PushResult): string => messages[result]
