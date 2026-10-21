<script setup lang="ts">
// The support pack's menu (ADR-171): the Inbox of tickets, where messages
// come from, and the data its agents use (customers, playbooks).
const props = defineProps<{ projectId: string }>()
const { t } = useLang()
const route = useRoute()
const view = computed({
  get: () => (['inbox', 'sources', 'data', 'report', 'settings'].includes(String(route.query.view)) ? String(route.query.view) : 'inbox'),
  set: v => navigateTo({ query: { ...route.query, view: v, ticket: undefined } }, { replace: true })
})
const items = computed(() => [
  { value: 'inbox', label: t('sup.inbox'), icon: 'i-lucide-inbox' },
  { value: 'sources', label: t('sup.sources'), icon: 'i-lucide-plug' },
  { value: 'data', label: t('sup.data'), icon: 'i-lucide-database' },
  { value: 'report', label: t('sup.report'), icon: 'i-lucide-chart-column' },
  { value: 'settings', label: t('sup.settings'), icon: 'i-lucide-settings-2' }
])
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-3">
    <SegmentedNav v-model="view" :items="items" class="shrink-0 self-start" />
    <SupportInbox v-if="view === 'inbox'" :project-id="props.projectId" />
    <SupportSources v-else-if="view === 'sources'" :project-id="props.projectId" />
    <DataCollections v-else-if="view === 'data'" :project-id="props.projectId" />
    <SupportReport v-else-if="view === 'report'" :project-id="props.projectId" />
    <SupportSettings v-else :project-id="props.projectId" />
  </div>
</template>
