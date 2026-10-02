<script setup lang="ts">
// Watch screen (like iTerm): chats of any projects side by side, split right
// or below, each box kept in this browser.
const { t } = useLang()
const { root, count, add, addEdge, clear } = useWatchLayout()
const { data: proj } = useLiveFetch<{ projects: Project[] }>('/api/projects', { lazy: true, server: false })
const projects = computed(() => proj.value?.projects ?? [])
const addMenu = computed(() => [projects.value.length
  ? projects.value.map(p => ({ label: p.name, icon: p.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder', onSelect: () => add(p.id) }))
  : [{ label: t('watch.noProjects'), disabled: true }]])
// the dashed strips above and below the boxes: a box across the whole width there
const edgeMenu = (edge: 'top' | 'bottom') => [projects.value.length
  ? projects.value.map(p => ({ label: p.name, icon: p.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder', onSelect: () => addEdge(p.id, edge) }))
  : [{ label: t('watch.noProjects'), disabled: true }]]
// an empty screen: the latest chats across projects, one click opens a box on it
interface RecentChat { id: string, project_id: string, title: string, agent_name: string, source?: Source, updated_at: string, active_turn?: string }
const { data: recentData } = useLiveFetch<{ conversations: RecentChat[], projects: Record<string, string> }>('/api/conversations/recent', { query: { limit: 12 }, lazy: true, server: false })
const recent = computed(() => recentData.value?.conversations ?? [])
const { dateLocale } = useLang()
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
function reset() {
  if (confirm(t('watch.clearConfirm'))) clear()
}
</script>

<template>
  <UDashboardPanel :ui="{ body: 'p-2 sm:p-2 min-h-0 overflow-hidden' }">
    <template #header>
      <UDashboardNavbar :title="t('watch.title')" :ui="{ root: 'max-sm:px-3' }">
        <template #leading>
          <UDashboardSidebarCollapse />
        </template>
        <template #right>
          <UButton v-if="count" size="sm" color="neutral" variant="ghost" icon="i-lucide-eraser" :label="t('watch.clear')" @click="reset" />
          <UDropdownMenu :items="addMenu" :content="{ align: 'end' }" :ui="{ content: 'max-h-80 w-60' }">
            <UButton size="sm" icon="i-lucide-plus" :label="t('watch.add')" />
          </UDropdownMenu>
        </template>
      </UDashboardNavbar>
    </template>
    <template #body>
      <div v-if="!proj" class="grid h-full grid-cols-2 gap-2" aria-busy="true">
        <USkeleton class="h-full" /><USkeleton class="h-full" />
      </div>
      <div v-else-if="root" class="flex h-full min-h-0 flex-col gap-1">
        <template v-for="edge in (['top', 'bottom'] as const)" :key="edge">
          <WatchTree v-if="edge === 'bottom'" :node="root" :projects="projects" class="min-h-0 flex-1" />
          <UDropdownMenu :items="edgeMenu(edge)" :content="{ align: 'center' }" :ui="{ content: 'max-h-80 w-60' }">
            <button
              type="button" class="flex h-6 w-full shrink-0 items-center justify-center rounded-md border border-dashed border-(--ui-border-accented) text-(--ui-text-dimmed) transition hover:border-(--ui-primary) hover:text-(--ui-primary)"
              :aria-label="t(edge === 'top' ? 'watch.addTop' : 'watch.addBottom')" :title="t(edge === 'top' ? 'watch.addTop' : 'watch.addBottom')"
            >
              <UIcon name="i-lucide-plus" class="size-4" />
            </button>
          </UDropdownMenu>
        </template>
      </div>
      <div v-else class="flex h-full flex-col items-center gap-3 overflow-y-auto py-8 text-center text-sm text-(--ui-text-muted)">
        <UIcon name="i-lucide-layout-grid" class="size-10 shrink-0 text-(--ui-text-dimmed)" />
        <p>{{ t('watch.empty') }}</p>
        <UDropdownMenu :items="addMenu" :content="{ align: 'center' }" :ui="{ content: 'max-h-80 w-60' }">
          <UButton icon="i-lucide-plus" :label="t('watch.add')" />
        </UDropdownMenu>
        <!-- the latest chats: one click opens a box on it -->
        <UCard v-if="!recentData || recent.length" class="mt-2 w-full max-w-md text-start" :ui="{ body: 'p-0 sm:p-0' }">
          <template #header>
            <p class="font-semibold text-(--ui-text)">{{ t('watch.recent') }}</p>
          </template>
          <LoadingRows v-if="!recentData" :n="4" />
          <div class="divide-y divide-(--ui-border)">
            <button
              v-for="c in recent" :key="c.id" type="button" class="flex w-full items-center gap-3 px-4 py-2.5 text-start transition hover:bg-(--ui-bg-elevated)"
              @click="add(c.project_id, c.id)"
            >
              <UIcon
                :name="c.active_turn ? 'i-lucide-loader-circle' : c.source && c.source !== 'web' ? sourceIcon[c.source] : 'i-lucide-messages-square'"
                class="size-4 shrink-0 text-(--ui-text-muted)" :class="{ 'animate-spin text-primary': c.active_turn }"
              />
              <div class="min-w-0 flex-1">
                <p class="truncate text-sm font-medium text-(--ui-text)">{{ c.title || t('chat.newThreadTitle') }}</p>
                <p class="truncate text-xs">{{ recentData?.projects[c.project_id] ?? c.project_id }} · {{ c.agent_name }} · {{ when(c.updated_at) }}</p>
              </div>
              <UIcon name="i-lucide-plus" class="size-4 shrink-0" />
            </button>
          </div>
        </UCard>
      </div>
    </template>
  </UDashboardPanel>
</template>
