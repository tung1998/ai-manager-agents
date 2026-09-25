<script setup lang="ts">
// Skills and MCP servers kept by office, plus what is installed machine-wide.
// Installing into a project is done from the project's "Skills & MCP" tab.
const kind = ref<ItemKind>('skill')
</script>

<template>
  <PageShell title="Thư viện">
    <div class="space-y-4">
      <UTabs
        v-model="kind" :content="false" class="w-fit"
        :items="[{ label: 'Skills', value: 'skill', icon: 'i-lucide-sparkles' }, { label: 'MCP servers', value: 'mcp', icon: 'i-lucide-plug-zap' }]"
      />
      <ToolsPanel :key="kind" :kind="kind" />
    </div>
  </PageShell>
</template>
