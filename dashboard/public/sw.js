// Browser notifications of what needs a person (plugins/notify.client.ts
// shows them): their buttons act right from the notification (Duyệt, Bỏ qua,
// Đã xem…, as composables/useApprovals.ts), with the dashboard's own session
// cookie; a click elsewhere opens where it is.
self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()))

function request(act, x) {
  switch (act) {
    case 'approve':
    case 'reject':
      return [(x.kind === 'patch' ? '/api/patches/' : '/api/actions/') + x.id + '/' + act, act === 'reject' ? { skip: true } : {}]
    case 'seen':
      return ['/api/conversations/' + x.id + '/seen', { seen: true }]
    case 'continue':
      return ['/api/conversations/' + x.id + '/messages', { text: x.prompt }]
    case 'dismiss':
      return ['/api/incidents/dismiss', { key: x.key }]
  }
  return null
}

self.addEventListener('notificationclick', (event) => {
  const n = event.notification
  const d = n.data || {}
  const x = d.incident || {}
  n.close()
  event.waitUntil((async () => {
    const req = request(event.action, x)
    if (req) {
      try {
        const res = await fetch(req[0], {
          method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(req[1])
        })
        if (res.ok) return // done: the notification is gone, nothing more to show
      } catch { /* offline: open it instead */ }
    }
    const link = x.link || '/'
    const tabs = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    const tab = tabs.find(c => c.url.startsWith(self.location.origin))
    if (tab) {
      await tab.focus()
      tab.postMessage({ type: 'open', link })
    } else {
      await self.clients.openWindow(new URL(link, self.location.origin).href)
    }
  })())
})
