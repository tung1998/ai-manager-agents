<script setup lang="ts">
// The project's Chat, from any page of the project (ADR-042): same threads
// and agent, and each message tells the agent which page is open.
const route = useRoute()
const { t } = useLang()
const open = ref(false)
const projectId = computed(() => route.params.id as string)
const hidden = computed(() => {
  const p = route.path
  if (!projectId.value || !p.startsWith(`/projects/${projectId.value}`)) return true
  if (p === `/projects/${projectId.value}` && (!route.query.tab || route.query.tab === 'chat')) return true // the Chat tab itself
  return p.endsWith('/automations/new') || p.endsWith('/edit') // the builder has its own chat
})
const pageContext = () => {
  const q = route.query
  return JSON.stringify({
    page: String(route.name ?? ''), path: route.path,
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
      icon="i-lucide-message-circle" size="xl" class="fixed bottom-5 end-5 z-40 rounded-full shadow-lg"
      :aria-label="t('floating.open')" :title="t('floating.open')" @click="open = true"
    />
    <USlideover v-model:open="open" :title="t('floating.title')" side="right" :ui="{ content: 'max-w-lg', body: 'p-0 sm:p-0 flex flex-col' }">
      <template #body>
        <div class="min-h-0 flex-1 p-2">
          <ChatPanel :key="projectId" :project-id="projectId" compact :page-context="pageContext" />
        </div>
      </template>
    </USlideover>
  </template>
</template>
