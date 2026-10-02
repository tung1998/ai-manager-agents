<script setup lang="ts">
// What a watch box shows before it has a chat (the empty screen, a fresh
// split): pick a project, or one of the latest chats across projects.
defineProps<{ projects: Project[], hint: string, icon?: string }>()
const emit = defineEmits<{ pick: [projectId: string, conversationId?: string] }>()
const { t, dateLocale } = useLang()

interface RecentChat { id: string, project_id: string, title: string, agent_name: string, source?: Source, updated_at: string, active_turn?: string }
const { data: recentData } = useLiveFetch<{ conversations: RecentChat[], projects: Record<string, string> }>('/api/conversations/recent', { query: { limit: 12 }, lazy: true, server: false })
const recent = computed(() => recentData.value?.conversations ?? [])
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
</script>

<template>
  <div class="flex flex-col items-center gap-3 overflow-y-auto px-2 py-8 text-center text-sm text-(--ui-text-muted)">
    <UIcon :name="icon ?? 'i-lucide-layout-grid'" class="size-10 shrink-0 text-(--ui-text-dimmed)" />
    <p>{{ hint }}</p>
    <div class="flex items-center gap-1">
      <UDropdownMenu
        :items="[projects.length
          ? projects.map(p => ({ label: p.name, icon: p.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder', onSelect: () => emit('pick', p.id) }))
          : [{ label: t('watch.noProjects'), disabled: true }]]"
        :content="{ align: 'center' }" :ui="{ content: 'max-h-80 w-60' }"
      >
        <UButton icon="i-lucide-folder" :label="t('watch.project')" />
      </UDropdownMenu>
      <slot name="actions" />
    </div>
    <!-- the latest chats: one click opens the box on it -->
    <UCard v-if="!recentData || recent.length" class="mt-2 w-full max-w-md text-start" :ui="{ body: 'p-0 sm:p-0' }">
      <template #header>
        <p class="font-semibold text-(--ui-text)">{{ t('watch.recent') }}</p>
      </template>
      <LoadingRows v-if="!recentData" :n="4" />
      <div class="divide-y divide-(--ui-border)">
        <button
          v-for="c in recent" :key="c.id" type="button" class="flex w-full items-center gap-3 px-4 py-2.5 text-start transition hover:bg-(--ui-bg-elevated)"
          @click="emit('pick', c.project_id, c.id)"
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
