<script setup lang="ts">
// Writing a project's own workflow (?w= one of its own, none: a new one)
// with an agent of the project; ?c= reopens its chat.
const route = useRoute()
const { t } = useLang()
const projectId = computed(() => route.params.id as string)
const id = computed(() => typeof route.query.w === 'string' ? route.query.w : '')
const { data } = await useLiveFetch<{ workflow: ProjectWorkflow }>(() => `/api/workflows/${id.value}`, { immediate: !!id.value })
</script>

<template>
  <PageShell :title="data?.workflow ? `/${data.workflow.key}` : t('wf.new')">
    <UButton :to="{ path: `/projects/${projectId}`, query: { tab: 'workflows' } }" icon="i-lucide-arrow-left" size="xs" color="neutral" variant="ghost" class="-ms-2 mb-2" :label="t('wf.section')" />
    <WorkflowEditor :key="id" :chat-project-id="projectId" :project-id="projectId" :workflow-id="id || undefined" />
  </PageShell>
</template>
