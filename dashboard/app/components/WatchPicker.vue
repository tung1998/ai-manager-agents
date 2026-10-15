<script setup lang="ts">
// What a watch box shows before it has anything (the empty screen, a fresh
// split): the widgets to pick from, then the latest chats across projects
// (one click opens it here).
import type { WidgetKind } from '~/utils/watchWidgets'

defineProps<{ projects: Project[], hint: string }>()
const emit = defineEmits<{ pick: [projectId: string, conversationId?: string], widget: [kind: WidgetKind] }>()
const { t, when } = useLang()
const { isAdmin } = useAuth()

interface RecentChat { id: string, project_id: string, title: string, agent_name: string, source?: Source, updated_at: string, active_turn?: string }
const { data: recentData, error: recentError } = useLiveFetch<{ conversations: RecentChat[], projects: Record<string, string> }>('/api/conversations/recent', { query: { limit: 20 }, lazy: true, server: false })
const recent = computed(() => recentData.value?.conversations ?? [])
// grouped by what they are about, small first
const gallery = computed(() => widgetGroups.map(group => ({ group, list: groupWidgets(group, isAdmin.value) })).filter(g => g.list.length))
</script>

<template>
  <div class="flex min-h-0 flex-col overflow-hidden rounded-lg border border-(--ui-border)">
    <div class="flex items-center gap-1 border-b border-(--ui-border) p-2">
      <UIcon name="i-lucide-square-dashed" class="mx-1.5 size-4 shrink-0 text-(--ui-text-dimmed)" />
      <p class="min-w-0 flex-1 truncate text-sm text-(--ui-text-muted)">{{ hint }}</p>
      <UDropdownMenu
        :items="[projects.length
          ? projects.map(p => ({ label: p.name, icon: p.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder', onSelect: () => emit('pick', p.id) }))
          : [{ label: t('watch.noProjects'), disabled: true }]]"
        :content="{ align: 'end' }" :ui="{ content: 'max-h-80 w-60' }"
      >
        <UButton size="sm" color="neutral" variant="soft" icon="i-lucide-messages-square" trailing-icon="i-lucide-chevron-down" :label="t('watchW.kind.chat')" />
      </UDropdownMenu>
      <slot name="actions" />
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto p-2">
      <template v-for="g in gallery" :key="g.group">
        <p class="flex items-center gap-1.5 px-2 pb-1 text-xs font-medium text-(--ui-text-dimmed)">
          <UIcon :name="groupIcon[g.group]" class="size-3.5" />
          {{ t(`watchW.group.${g.group}`) }}
        </p>
        <div class="grid gap-1.5 pb-3 [grid-template-columns:repeat(auto-fill,minmax(10rem,1fr))]">
          <button
            v-for="w in g.list" :key="w.kind" type="button"
            class="group flex items-start gap-2 rounded-md border border-(--ui-border) p-2 text-start transition hover:border-(--ui-primary) hover:bg-(--ui-primary)/5"
            @click="emit('widget', w.kind)"
          >
            <UIcon :name="w.icon" class="mt-0.5 size-4 shrink-0 text-(--ui-text-muted) group-hover:text-(--ui-primary)" />
            <span class="min-w-0">
              <span class="flex items-center gap-1.5">
                <span class="truncate text-sm font-medium">{{ t(`watchW.kind.${w.kind}` as 'watchW.kind.chat') }}</span>
                <span class="shrink-0 rounded bg-(--ui-bg-elevated) px-1 text-[10px] text-(--ui-text-muted) tabular-nums">{{ w.grid.w }}×{{ w.grid.h }}</span>
              </span>
              <span class="line-clamp-2 text-xs text-(--ui-text-muted)">{{ t(`watchW.desc.${w.kind}` as 'watchW.desc.chat') }}</span>
            </span>
          </button>
        </div>
      </template>
      <p class="px-2 pb-1 text-xs font-medium text-(--ui-text-dimmed)">{{ t('watch.recent') }}</p>
      <LoadingRows v-if="!recentData && !recentError" :n="4" />
      <p v-else-if="recentError" class="px-2 py-1 text-sm text-(--ui-error)">{{ t('watch.recentError') }}</p>
      <p v-else-if="!recent.length" class="px-2 py-1 text-sm text-(--ui-text-muted)">{{ t('chat.none') }}</p>
      <button
        v-for="c in recent" :key="c.id" type="button"
        class="group flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-start transition hover:bg-(--ui-bg-elevated)"
        @click="emit('pick', c.project_id, c.id)"
      >
        <UIcon
          :name="c.active_turn ? 'i-lucide-loader-circle' : c.source && c.source !== 'web' ? sourceIcon[c.source] : 'i-lucide-message-square'"
          class="size-4 shrink-0 text-(--ui-text-dimmed)" :class="{ 'animate-spin text-primary': c.active_turn }"
        />
        <span class="min-w-0 flex-1">
          <span class="block truncate text-sm">{{ c.title || t('chat.newThreadTitle') }}</span>
          <span class="block truncate text-xs text-(--ui-text-muted)">{{ recentData?.projects[c.project_id] ?? c.project_id }} · {{ c.agent_name }}</span>
        </span>
        <span class="shrink-0 text-xs text-(--ui-text-dimmed) tabular-nums group-hover:hidden">{{ when(c.updated_at) }}</span>
        <UIcon name="i-lucide-arrow-right" class="hidden size-4 shrink-0 text-(--ui-primary) group-hover:block" />
      </button>
    </div>
  </div>
</template>
