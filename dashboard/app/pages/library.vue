<script setup lang="ts">
// The library was split into its own menu items (Skills, MCP servers,
// Workflows): old links go to the one they meant.
const route = useRoute()
const q = route.query
const to = q.tab === 'workflows' || q.kind === 'workflow' ? '/workflows' : q.kind === 'mcp' || q.tab === 'mcp' ? '/mcp' : '/skills'
await navigateTo({ path: to, hash: route.hash }, { replace: true })
</script>

<template>
  <div />
</template>
