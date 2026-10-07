<script setup lang="ts">
// Skills kept by office, plus what is installed machine-wide. Installing
// into a project is done from the project's tabs.
const { t } = useLang()
</script>

<template>
  <PageShell :title="t('tools.skillsTab')">
    <ToolsPanel kind="skill" />
  </PageShell>
</template>
