// Whether FloatingChat's FAB shows on the current route (ADR-042/046).
// Shared with PageShell so it can reserve bottom padding only when the FAB covers it.
export function useFabVisible() {
  const route = useRoute()
  const projectId = computed(() => (route.params.id as string) || '')
  const inProject = computed(() => !!projectId.value && route.path.startsWith(`/projects/${projectId.value}`))
  const { data: asst } = useLiveFetch<{ project_id: string }>('/api/assistant', { lazy: true, server: false })
  const assistantId = computed(() => asst.value?.project_id ?? '')
  const hidden = computed(() => {
    const p = route.path
    if (p.startsWith('/assistant') || p === '/login' || p === '/watch') return true
    if (p === '/workflows/edit') return true // the library's editor has its own chat // the assistant's own page; the watch screen is all chats
    if (inProject.value) {
      if (p === `/projects/${projectId.value}` && (!route.query.tab || route.query.tab === 'chat' || route.query.tab === 'tasks')) return true // the Chat tab itself; Tasks has its own talk
      if (p.includes('/bots/') || p.endsWith('/skills/edit')) return true // a page with its own chat
      if (p.endsWith('/automations/new') || p.endsWith('/edit')) return true // the builder has its own chat
      return false
    }
    return !assistantId.value
  })
  return computed(() => !hidden.value)
}
