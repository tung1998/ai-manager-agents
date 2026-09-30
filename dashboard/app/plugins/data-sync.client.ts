// A change that succeeded (POST/PUT/PATCH/DELETE to /api) refreshes the lists
// on screen: no F5 after adding, editing or removing something. Sending a
// chat message, checking a draft or signing in are not changes to a list.
const notAChange = /\/api\/(auth\/|login|logout)|\/messages(\?|$)|\/validate(\?|$)|\/cancel(\?|$)|\/api\/automation\/(scan|content)|\/chat\/test/

export default defineNuxtPlugin(() => {
  const base = globalThis.$fetch
  globalThis.$fetch = base.create({
    onResponse({ request, options, response }) {
      const method = String(options.method ?? 'GET').toUpperCase()
      const url = typeof request === 'string' ? request : request instanceof Request ? request.url : String(request)
      if (method !== 'GET' && response.ok && url.includes('/api/') && !notAChange.test(url)) dataChanged()
    }
  }) as typeof base
  // back on the tab: what others (or an agent) changed meanwhile
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') dataChanged()
  })
})
