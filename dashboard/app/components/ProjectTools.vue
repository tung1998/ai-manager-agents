<script setup lang="ts">
// "Skills & MCP" tab of a project.
defineProps<{ projectPath: string }>()
const kind = ref<ItemKind>('skill')
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-3">
      <UTabs
        v-model="kind" :content="false" class="w-fit"
        :items="[{ label: 'Skills', value: 'skill', icon: 'i-lucide-sparkles' }, { label: 'MCP servers', value: 'mcp', icon: 'i-lucide-plug-zap' }]"
      />
      <p class="text-xs text-(--ui-text-muted)">
        Dùng khi mở Claude Code trong project. Skill/MCP cài "Chia sẻ" nằm trong repo, commit để cả team dùng.
      </p>
    </div>
    <ToolsPanel :key="kind" :kind="kind" :project-path="projectPath" />
  </div>
</template>
