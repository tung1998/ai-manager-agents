// A new card waiting for approval (a proposal, a diff) is announced at once:
// a toast with Duyệt/Bỏ qua when the tab is in view, a browser notification
// with the same buttons (public/sw.js) when it is not. The list is the
// 'incidents' data office pushes live (useDataSync); what was there when the
// page opened is not announced again.
import { decideApproval, isApproval, useBrowserNotify, type Incident } from '~/composables/useApprovals'

export default defineNuxtPlugin(() => {
  const { supported, permission } = useBrowserNotify()
  const { t } = useLang()
  const toast = useToast()
  const route = useRoute()
  let sw: ServiceWorkerRegistration | undefined
  if (supported) {
    navigator.serviceWorker.register('/sw.js').then((r) => { sw = r }).catch(() => {})
    navigator.serviceWorker.addEventListener('message', (e) => { // a notification clicked: open where it is
      if (e.data?.type === 'open' && typeof e.data.link === 'string') navigateTo(e.data.link)
    })
  }

  const seen = new Set<string>()
  let primed = false
  const { data } = useNuxtData<{ incidents?: Incident[] }>('incidents')
  watch(() => data.value?.incidents, (list) => {
    if (!list) return
    const fresh = list.filter(x => isApproval(x) && !seen.has(x.key))
    for (const x of fresh) seen.add(x.key)
    if (!primed) { // the cards there when the page opened: the header lists them
      primed = true
      return
    }
    for (const x of fresh.slice(0, 3)) announce(x)
  }, { immediate: true })

  function announce(x: Incident) {
    const title = t('bell.waiting', { project: x.project_name })
    if (document.visibilityState === 'visible') {
      if (x.link && route.fullPath.includes(x.link.split('?')[1] ?? '\0')) return // that chat is open: its card is right there
      toast.add({
        title, description: x.title, color: 'warning', icon: 'i-lucide-bell-ring', duration: 20_000,
        actions: [
          { label: t('bell.approve'), color: 'primary', onClick: () => { void decide(x, 'approve') } },
          { label: t('bell.skip'), color: 'neutral', variant: 'outline', onClick: () => { void decide(x, 'reject') } },
          { label: t('bell.open'), color: 'neutral', variant: 'ghost', onClick: () => { navigateTo(x.link) } }
        ]
      })
      return
    }
    if (!sw || permission.value !== 'granted') return
    void sw.showNotification(title, {
      body: x.title, tag: x.key,
      data: { kind: x.kind, id: x.id, link: x.link, approved: t('bell.approved'), rejected: t('bell.skipped') },
      // @ts-expect-error notification buttons: Chrome/Edge show them (not in the DOM lib's options type)
      actions: [{ action: 'approve', title: t('bell.approve') }, { action: 'reject', title: t('bell.skip') }]
    })
  }

  async function decide(x: Incident, what: 'approve' | 'reject') {
    try {
      await decideApproval(x, what)
      toast.add({ title: what === 'approve' ? t('bell.approved') : t('bell.skipped'), color: 'success' })
    } catch (e) {
      toast.add({ title: apiError(e), color: 'error' })
    }
  }
})
