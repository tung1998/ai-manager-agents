// Admin dashboard for agent-office. Runs as a SPA; every /api call goes
// through the Nitro proxy in server/api/[...].ts so the session cookie stays
// same-origin with the page.
export default defineNuxtConfig({
  compatibilityDate: '2026-09-01',
  ssr: false,
  modules: ['@nuxt/ui'],
  css: ['~/assets/css/main.css'],
  devtools: { enabled: false },
  app: {
    head: {
      title: 'agent-office',
      htmlAttrs: { lang: 'vi' }
    }
  },
  runtimeConfig: {
    // Override with NUXT_OFFICE_API_BASE, e.g. http://office:8787 in docker.
    officeApiBase: 'http://127.0.0.1:8787'
  },
  icon: {
    // /api/** is proxied to the Go server, so icons must not live under /api.
    localApiEndpoint: '/_nuxt_icon',
    clientBundle: { scan: true }
  },
  devServer: { port: 2704 }
})
