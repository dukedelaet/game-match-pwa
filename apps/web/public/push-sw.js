// Web Push handlers, imported into the generated service worker via
// workbox.importScripts (see vite.config.ts).

self.addEventListener('push', (event) => {
  let data = { title: 'GameMatch', body: 'Something happened.' }
  try {
    if (event.data) data = event.data.json()
  } catch {
    // Non-JSON payloads fall back to the default copy.
  }
  event.waitUntil(
    self.registration.showNotification(data.title || 'GameMatch', {
      body: data.body || '',
      tag: data.tag,
      data: { url: data.url || '/' },
      icon: '/favicon.svg',
      badge: '/favicon.svg',
    }),
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = (event.notification.data && event.notification.data.url) || '/'
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
      for (const client of list) {
        if ('focus' in client) return client.focus()
      }
      return self.clients.openWindow(url)
    }),
  )
})
