// A session can end after auth.global.ts checked it once (7-day expiry, an
// admin locking the account, logout in another tab): the API then answers
// 401 to every call and the page looks fine but does nothing. Any 401 from
// the API outside /api/auth/ means the session is gone (server.go
// requireAuth): back to the login page, coming back here after.
// Wraps window.fetch for the reason data-sync.client.ts gives.
export default defineNuxtPlugin((nuxtApp) => {
  const { user } = useAuth()
  const base = window.fetch
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const res = await base(input, init)
    const path = requestURL(input).replace(/^https?:\/\/[^/]+/, '')
    if (res.status === 401 && path.startsWith('/api/') && !path.startsWith('/api/auth/') && user.value) {
      user.value = null // once, however many calls fail together
      stopLive()
      const from = nuxtApp.$router.currentRoute.value.fullPath
      void nuxtApp.runWithContext(() => navigateTo({ path: '/login', query: from !== '/' && from !== '/login' ? { redirect: from } : {} }))
    }
    return res
  }
})
