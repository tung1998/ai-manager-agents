<script setup lang="ts">
// MCP servers: office's gateway and the library, plus what is installed
// machine-wide. Installing into a project is done from the project's tabs.
const { t } = useLang()
</script>

<template>
  <PageShell :title="t('tools.mcpTab')">
    <ToolsPanel kind="mcp" />
  </PageShell>
</template>
