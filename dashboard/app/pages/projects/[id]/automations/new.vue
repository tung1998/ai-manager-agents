<script setup lang="ts">
const route = useRoute()
const { t } = useLang()
const projectId = computed(() => route.params.id as string)
</script>

<template>
  <PageShell :title="t('auto.new')">
    <UButton :to="{ path: `/projects/${projectId}`, query: { tab: 'automations' } }" icon="i-lucide-arrow-left" size="xs" color="neutral" variant="ghost" class="-ms-2 mb-2" :label="t('auto.back')" />
    <AutomationBuilder :project-id="projectId" />
  </PageShell>
</template>
