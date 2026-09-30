// A change that succeeded (POST/PUT/PATCH/DELETE to /api) refreshes the lists
// on screen: no F5 after adding, editing or removing something. Sending a
// chat message, checking a draft or signing in are not changes to a list.
// a change that starts work: its end changes the lists again (green, red)
const startsWork = /\/(run|retry|rerun|restart|approve)(\?|$)|\/incidents\/retry/
const notAChange = /\/api\/(auth\/|login|logout)|\/messages(\?|$)|\/validate(\?|$)|\/cancel(\?|$)|\/api\/automation\/(scan|content)|\/chat\/test/

export default defineNuxtPlugin(() => {
  const base = globalThis.$fetch
  globalThis.$fetch = base.create({
    onResponse({ request, options, response }) {
      const method = String(options.method ?? 'GET').toUpperCase()
      const url = typeof request === 'string' ? request : request instanceof Request ? request.url : String(request)
      if (method !== 'GET' && response.ok && url.includes('/api/') && !notAChange.test(url)) {
        dataChanged()
        if (startsWork.test(url)) followWork()
      }
    }
  }) as typeof base
  // back on the tab: what others (or an agent) changed meanwhile
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') dataChanged()
  })
})

// followWork: while jobs run (after a Run/Retry here), the lists refresh each
// time how many are running changes, and once more when all are done
let following = false
async function followWork() {
  if (following) return
  following = true
  let last = -1
  const until = Date.now() + 15 * 60_000
  try {
    await new Promise(r => setTimeout(r, 1500))
    while (Date.now() < until) {
      const s = await globalThis.$fetch<{ totals: { running: number, pending: number } }>('/api/jobs/stats?since=24h')
      const n = s.totals.running + s.totals.pending
      if (last !== -1 && n !== last) dataChanged()
      if (n === 0) { dataChanged(); break } // all done (a quick script: already at the first look)
      last = n
      await new Promise(r => setTimeout(r, 3000))
    }
  } catch { /* signed out or offline: the next change starts it again */ }
  finally {
    following = false
  }
}
