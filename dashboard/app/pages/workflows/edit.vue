<script setup lang="ts">
// Writing a library workflow (?key= one there, none: a new one) with the
// office assistant; ?c= reopens its chat.
const route = useRoute()
const { t } = useLang()
const key = computed(() => typeof route.query.key === 'string' ? route.query.key : '')
const { data: asst } = await useLiveFetch<{ project_id: string }>('/api/assistant', { key: 'assistant' })
</script>

<template>
  <PageShell :title="key ? `#${key}` : t('wf.new')">
    <UButton :to="'/workflows'" icon="i-lucide-arrow-left" size="xs" color="neutral" variant="ghost" class="-ms-2 mb-2" :label="t('wf.library')" />
    <WorkflowEditor v-if="asst?.project_id" :key="key" :chat-project-id="asst.project_id" :lib-key="key || undefined" />
  </PageShell>
</template>
