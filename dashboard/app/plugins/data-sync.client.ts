// What this page changed shows at once, even before the server's notice
// (ADR-072): a successful write refreshes what shows its tables; a tab back
// into view refreshes all (its stream may have slept).
//
// It wraps window.fetch, not $fetch: Nuxt binds the auto-imported $fetch to
// globalThis.$fetch once, when its module loads (`export const $fetch =
// globalThis.$fetch`), so a plugin replacing globalThis.$fetch reaches no
// call. ofetch calls globalThis.fetch on every request, so this does.
export default defineNuxtPlugin(() => {
  const base = window.fetch
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const res = await base(input, init)
    const method = String(init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase()
    const url = requestURL(input)
    // a check that writes nothing (validate, test, preview) refreshes nothing
    if (method !== 'GET' && res.ok && url.includes('/api/') && !/\/(validate|test|preview|scan|content)(\?|$)/.test(url)) {
      const tables = tablesFor(url.replace(/^https?:\/\/[^/]+/, ''))
      if (tables.length) liveChanged(tables)
    }
    return res
  }
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') liveChanged(['*'])
  })
})
