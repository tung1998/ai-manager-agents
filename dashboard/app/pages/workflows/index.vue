<script setup lang="ts">
// The workflow library (ADR-098): shared workflows a project installs a copy of.
const { t } = useLang()
</script>

<template>
  <PageShell :title="t('wf.library')">
    <WorkflowLibrary />
  </PageShell>
</template>
