// Something new that needs a person (a card to approve, a chat answered, a
// run that failed…) is announced at once, both ways, each with its buttons:
// a toast in the top corner (an approval's stays until it is decided) and a
// browser notification (public/sw.js), when the person allowed them. Decided
// or gone elsewhere, both close. The list is the 'incidents' data office
// pushes live (useDataSync); what was there when the page opened is not
// announced again.
import { actOnIncident, alreadyDecided, alreadyDecidedText, incidentActs, incidentText, isApproval, useBrowserNotify, type Incident, type IncidentAct } from '~/composables/useApprovals'

const maxAtOnce = 3 // more at once: one toast saying how many
// a card is announced only if it still waits a moment later: one office
// approves on its own (an agent's permission, a bot's direct mode) or someone
// decides elsewhere right away leaves the list first, so nobody is asked
const approvalGrace = 2500

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

  const label = (a: IncidentAct | 'open') => t(`bell.act.${a}`)
  const titleOf = (x: Incident) => isApproval(x) ? t('bell.waiting', { project: x.project_name }) : `${x.project_name || 'office'}: ${t(`incidents.kind.${x.kind}` as 'incidents.kind.monitor')}`

  const seen = new Set<string>()
  const toasts = new Map<string, string | number>() // incident key → its toast
  const waiting = new Map<string, ReturnType<typeof setTimeout>>() // approvals not announced yet (approvalGrace)
  let primed = false
  const { data } = useNuxtData<{ incidents?: Incident[] }>('incidents')
  const stillThere = (key: string) => !!data.value?.incidents?.some(x => x.key === key)
  watch(() => data.value?.incidents, (list) => {
    if (!list) return
    const now = new Set(list.map(x => x.key))
    for (const key of [...toasts.keys(), ...waiting.keys()]) { // decided or gone meanwhile: its toast and notification go too
      if (!now.has(key)) dismiss(key)
    }
    const fresh = list.filter(x => !seen.has(x.key))
    for (const x of fresh) seen.add(x.key)
    if (!primed) { // what was there when the page opened: the header lists it
      primed = true
      return
    }
    for (const x of fresh.slice(0, maxAtOnce)) {
      if (!isApproval(x)) {
        announce(x)
        continue
      }
      waiting.set(x.key, setTimeout(() => {
        waiting.delete(x.key)
        if (stillThere(x.key)) announce(x)
      }, approvalGrace))
    }
    if (fresh.length > maxAtOnce) {
      toast.add({ title: t('bell.more', { n: fresh.length - maxAtOnce }), color: 'warning', icon: 'i-lucide-bell-ring', actions: [{ label: t('bell.all'), onClick: () => { navigateTo('/') } }] })
    }
  }, { immediate: true })

  function dismiss(key: string) {
    clearTimeout(waiting.get(key))
    waiting.delete(key)
    const id = toasts.get(key)
    if (id !== undefined) toast.remove(id)
    toasts.delete(key)
    void sw?.getNotifications({ tag: key }).then(ns => ns.forEach(n => n.close()))
  }

  // a diff of hundreds of files: how many and the first few, never the whole list
  const short = (x: Incident) => incidentText(x, (n, list) => t('bell.files', { n, list }))

  function announce(x: Incident) {
    const title = titleOf(x)
    const acts = incidentActs(x)
    // the chat it is about is open and in view: its card is right there
    const here = document.visibilityState === 'visible' && !!x.link && route.fullPath.includes(x.link.split('?')[1] ?? '\0')
    if (!here) {
      const id = `inc-${x.key}`
      toast.add({
        id, title, description: short(x), ui: { description: 'line-clamp-2 break-words' }, icon: isApproval(x) ? 'i-lucide-bell-ring' : 'i-lucide-bell',
        color: x.severity === 'error' ? 'error' : isApproval(x) ? 'warning' : 'info',
        duration: isApproval(x) ? 0 : 30_000, // a card to decide stays until it is
        actions: [
          ...acts.map((a, i) => ({ label: label(a), color: (i === 0 ? 'primary' : 'neutral') as 'primary' | 'neutral', variant: (i === 0 ? 'solid' : 'outline') as 'solid' | 'outline', onClick: () => { void act(x, a) } })),
          { label: label('open'), color: 'neutral' as const, variant: 'ghost' as const, onClick: () => { navigateTo(x.link || '/') } }
        ]
      })
      toasts.set(x.key, id)
    }
    if (!sw || permission.value !== 'granted') return
    void sw.showNotification(title, {
      body: short(x), tag: x.key, requireInteraction: isApproval(x),
      data: { incident: x },
      // @ts-expect-error notification buttons (Chrome/Edge; not in the DOM lib's options type)
      actions: [...acts.map(a => ({ action: a, title: label(a) })), { action: 'open', title: label('open') }].slice(0, 2)
    })
  }

  // done: its toast and notification just go, no other notice; decided
  // elsewhere first: they go too, saying by whom
  async function act(x: Incident, what: IncidentAct) {
    try {
      await actOnIncident(x, what)
      dismiss(x.key)
    } catch (e) {
      const d = alreadyDecided(e)
      if (d) dismiss(x.key)
      toast.add(d ? { title: alreadyDecidedText(d, t), color: 'info' } : { title: apiError(e), color: 'error' })
    }
  }
})
