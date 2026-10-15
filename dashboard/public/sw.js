// Browser notifications of what waits for a person (plugins/notify.client.ts
// shows them): "Duyệt"/"Bỏ qua" decide it right from the notification, with
// the dashboard's own session cookie; a click elsewhere opens where it is.
self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()))

self.addEventListener('notificationclick', (event) => {
  const n = event.notification
  const d = n.data || {}
  n.close()
  event.waitUntil((async () => {
    if ((event.action === 'approve' || event.action === 'reject') && d.id) {
      const base = d.kind === 'patch' ? '/api/patches/' : '/api/actions/'
      try {
        const res = await fetch(base + d.id + '/' + event.action, {
          method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(event.action === 'reject' ? { skip: true } : {})
        })
        if (res.ok) {
          await self.registration.showNotification(event.action === 'approve' ? d.approved : d.rejected, { body: n.title, tag: n.tag, silent: true })
          return
        }
      } catch { /* offline: open it instead */ }
    }
    const url = new URL(d.link || '/', self.location.origin).href
    const tabs = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    const tab = tabs.find(c => c.url.startsWith(self.location.origin))
    if (tab) {
      await tab.focus()
      tab.postMessage({ type: 'open', link: d.link || '/' })
    } else {
      await self.clients.openWindow(url)
    }
  })())
})
