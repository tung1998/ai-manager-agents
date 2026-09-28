<script setup lang="ts">
const route = useRoute()
const { t } = useLang()
const projectId = computed(() => route.params.id as string)
const { data } = await useFetch<{ automation: Automation }>(() => `/api/automations/${route.params.aid}`)
</script>

<template>
  <PageShell :title="data?.automation.name ?? t('auto.edit')">
    <UButton :to="`/projects/${projectId}/automations/${route.params.aid}`" icon="i-lucide-arrow-left" size="xs" color="neutral" variant="ghost" class="-ms-2 mb-2" :label="data?.automation.name ?? t('auto.back')" />
    <AutomationBuilder v-if="data" :key="data.automation.id" :project-id="projectId" :automation="data.automation" />
  </PageShell>
</template>
