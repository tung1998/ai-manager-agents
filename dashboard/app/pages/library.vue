<script setup lang="ts">
// Skills, MCP servers and workflows kept by office, plus what is installed
// machine-wide. Installing into a project is done from the project's tabs.
const { t } = useLang()
const route = useRoute()
const router = useRouter()
// ?kind=mcp: the MCP tab (office's gateway); ?tab=workflows: the workflows
const initial = route.query.tab === 'workflows' || route.query.kind === 'workflow' ? 'workflow' : route.query.kind === 'mcp' ? 'mcp' : 'skill'
const kind = ref<ItemKind | 'workflow'>(initial)
watch(kind, k => router.replace({ query: k === 'workflow' ? { tab: 'workflows' } : k === 'mcp' ? { kind: 'mcp' } : {} }))
const tabs = computed(() => [
  { label: t('tools.skillsTab'), value: 'skill', icon: 'i-lucide-sparkles' },
  { label: t('tools.mcpTab'), value: 'mcp', icon: 'i-lucide-plug-zap' },
  { label: t('wf.tab'), value: 'workflow', icon: 'i-lucide-workflow' }
])
</script>

<template>
  <PageShell :title="t('tools.libraryPageTitle')">
    <div class="space-y-4">
      <UTabs
        v-model="kind" :content="false" class="w-fit"
        :items="tabs"
      />
      <WorkflowLibrary v-if="kind === 'workflow'" />
      <ToolsPanel v-else :key="kind" :kind="kind" />
    </div>
  </PageShell>
</template>
