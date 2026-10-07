<script setup lang="ts">
// Skills and MCP servers kept by office, plus what is installed
// machine-wide. Installing into a project is done from the project's tabs.
const { t } = useLang()
const route = useRoute()
const router = useRouter()
// workflows have their own page now: old links go there
if (route.query.tab === 'workflows' || route.query.kind === 'workflow') await navigateTo('/workflows', { replace: true })
// ?kind=mcp: the MCP tab (office's gateway)
const kind = ref<ItemKind>(route.query.kind === 'mcp' ? 'mcp' : 'skill')
watch(kind, k => router.replace({ query: k === 'mcp' ? { kind: 'mcp' } : {} }))
const tabs = computed(() => [
  { label: t('tools.skillsTab'), value: 'skill', icon: 'i-lucide-sparkles' },
  { label: t('tools.mcpTab'), value: 'mcp', icon: 'i-lucide-plug-zap' }
])
</script>

<template>
  <PageShell :title="t('tools.libraryPageTitle')">
    <div class="space-y-4">
      <UTabs
        v-model="kind" :content="false" class="w-fit"
        :items="tabs"
      />
      <ToolsPanel :key="kind" :kind="kind" />
    </div>
  </PageShell>
</template>
