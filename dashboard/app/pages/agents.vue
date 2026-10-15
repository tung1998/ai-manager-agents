<script setup lang="ts">
// The agent library (ADR-153, ADR-154) on its own: browse, read one as a
// model, or add it to a project (its Agents tab opens with the form filled).
const { t } = useLang()
const { isAdmin } = useAuth()

const selected = ref<LibraryAgent>()
const { data: projData } = useLiveFetch<{ projects: Project[] }>('/api/projects', { lazy: true })
const projects = computed(() => (projData.value?.projects ?? []).map(p => ({ label: p.name, value: p.id })))
const projectId = ref('')
watch(projects, (l) => { if (!projectId.value && l[0]) projectId.value = l[0].value }, { immediate: true })

function addTo(a: LibraryAgent) {
  if (!projectId.value) return
  navigateTo({ path: `/projects/${projectId.value}`, query: { tab: 'agents', add: a.id } })
}
</script>

<template>
  <PageShell :title="t('team.lib.pageTitle')">
    <div class="max-w-4xl space-y-3">
      <p class="text-sm text-(--ui-text-muted)">{{ t('team.lib.pageDesc') }}</p>
      <AgentLibrary v-model:selected="selected" tall @pick="addTo">
        <template v-if="isAdmin" #actions="{ agent }">
          <div class="flex flex-wrap items-center justify-end gap-2 border-t border-(--ui-border) pt-2">
            <USelect v-model="projectId" :items="projects" :loading="!projData" :placeholder="t('team.lib.pickProject')" size="sm" class="w-full sm:w-56" />
            <UButton icon="i-lucide-user-plus" size="sm" :disabled="!projectId" :label="t('team.lib.toProject')" @click="addTo(agent)" />
          </div>
        </template>
      </AgentLibrary>
    </div>
  </PageShell>
</template>
