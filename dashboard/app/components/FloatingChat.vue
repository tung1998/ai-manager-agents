<script setup lang="ts">
// The chat in the corner (ADR-042/046): on a project's pages it is the
// project's Chat (with the page as context), switchable to the office
// assistant; elsewhere it is the office assistant.
const route = useRoute()
const { t } = useLang()
const open = ref(false)
const projectId = computed(() => (route.params.id as string) || '')
const inProject = computed(() => !!projectId.value && route.path.startsWith(`/projects/${projectId.value}`))
const { data: asst } = useFetch<{ project_id: string }>('/api/assistant', { lazy: true, server: false })
const assistantId = computed(() => asst.value?.project_id ?? '')
// the scope chip: the project, or the whole office
const scope = ref<'project' | 'office'>('project')
watch(inProject, v => { scope.value = v ? 'project' : 'office' }, { immediate: true })
const target = computed(() => scope.value === 'project' && inProject.value ? projectId.value : assistantId.value)
const hidden = computed(() => {
  const p = route.path
  if (p.startsWith('/assistant') || p === '/login') return true // the assistant's own page
  if (inProject.value) {
    if (p === `/projects/${projectId.value}` && (!route.query.tab || route.query.tab === 'chat' || route.query.tab === 'tasks')) return true // the Chat tab itself; Tasks has its own talk
    if (p.includes('/bots/') || p.endsWith('/skills/edit')) return true // a page with its own chat
    if (p.endsWith('/automations/new') || p.endsWith('/edit')) return true // the builder has its own chat
    return false
  }
  return !assistantId.value
})
const pageContext = () => {
  const q = route.query
  return JSON.stringify({
    page: String(route.name ?? ''), path: route.path, project_id: projectId.value,
    tab: q.tab ?? '', section: q.section ?? '', task_id: q.task ?? '',
    agent_id: route.params.agentId ?? '', automation_id: route.params.aid ?? '',
    title: typeof document !== 'undefined' ? document.title : ''
  })
}
watch(() => route.fullPath, () => { if (hidden.value) open.value = false })
</script>

<template>
  <template v-if="!hidden">
    <UButton
      :icon="target === assistantId ? 'i-lucide-sparkles' : 'i-lucide-message-circle'" size="xl" class="fixed bottom-5 end-5 z-40 rounded-full shadow-lg"
      :aria-label="t('floating.open')" :title="t('floating.open')" @click="open = true"
    />
    <USlideover v-model:open="open" :title="target === assistantId ? t('assistant.title') : t('floating.title')" side="right" :ui="{ content: 'max-w-lg', body: 'p-0 sm:p-0 flex flex-col' }">
      <template #body>
        <div v-if="inProject && assistantId" class="flex gap-1 border-b border-(--ui-border) p-2">
          <UButton size="xs" :color="scope === 'project' ? 'primary' : 'neutral'" :variant="scope === 'project' ? 'soft' : 'ghost'" icon="i-lucide-folder" :label="t('assistant.scopeProject')" @click="scope = 'project'" />
          <UButton size="xs" :color="scope === 'office' ? 'primary' : 'neutral'" :variant="scope === 'office' ? 'soft' : 'ghost'" icon="i-lucide-sparkles" :label="t('assistant.scopeOffice')" @click="scope = 'office'" />
        </div>
        <div class="min-h-0 flex-1 p-2">
          <ChatPanel v-if="target" :key="target" :project-id="target" compact :page-context="pageContext" />
        </div>
      </template>
    </USlideover>
  </template>
</template>
