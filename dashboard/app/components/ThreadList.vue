<script setup lang="ts">
import type { DropdownMenuItem } from '@nuxt/ui'

// The chats of a project, newest first: the chat's side column on a wide
// screen, a drawer on a phone.
interface Thread { id: string, agent_id: string, agent_name: string, title: string, updated_at: string, active_turn?: string, source?: Source, purpose?: string }
defineProps<{ loading?: boolean, conversations: Thread[], agents: { id: string, name: string }[], currentId?: string, menu: (c: Thread) => DropdownMenuItem[][] }>()
const origin = defineModel<'all' | Source>('origin', { default: 'all' })
const emit = defineEmits<{ open: [Thread], new: [] }>()
const { t, dateLocale } = useLang()
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="space-y-1.5 border-b border-(--ui-border) p-2">
      <UButton icon="i-lucide-square-pen" :label="t('chat.newThread')" size="sm" color="neutral" variant="ghost" block class="justify-start" @click="emit('new')" />
      <SourceFilter v-model="origin" />
    </div>
    <div class="flex-1 overflow-y-auto p-1">
      <LoadingRows v-if="loading && !conversations.length" :n="6" />
      <p v-else-if="!conversations.length" class="p-3 text-xs text-(--ui-text-muted)">{{ t('chat.none') }}</p>
      <div
        v-for="c in conversations" :key="c.id"
        class="group flex cursor-pointer items-start gap-1 rounded-md px-2 py-1.5 text-sm"
        :class="currentId === c.id ? 'bg-(--ui-bg-accented)' : 'hover:bg-(--ui-bg-muted)'"
        @click="emit('open', c)"
      >
        <AgentAvatar :agent="agents.find(a => a.id === c.agent_id) ?? { id: c.agent_id, name: c.agent_name }" size="xs" class="mt-0.5" />
        <div class="min-w-0 flex-1">
          <p class="truncate">{{ c.title || t('chat.newThreadTitle') }}</p>
          <p class="flex items-center gap-1 truncate text-xs text-(--ui-text-muted)">
            <UIcon v-if="c.source && c.source !== 'web'" :name="sourceIcon[c.source]" class="size-3 shrink-0" :title="t(`source.${c.source}`)" />
            <UIcon v-if="c.purpose === 'skill'" name="i-lucide-sparkles" class="size-3 shrink-0" :title="t('chat.skillChat')" />
            <span class="truncate">{{ c.agent_name }} · {{ when(c.updated_at) }}</span>
          </p>
        </div>
        <UIcon v-if="c.active_turn" name="i-lucide-loader-circle" class="mt-1 size-3.5 animate-spin text-(--ui-text-muted)" />
        <UDropdownMenu :items="menu(c)" :content="{ align: 'end' }">
          <!-- visible on a phone (no hover there) -->
          <button type="button" class="-me-1 rounded px-0.5 text-(--ui-text-dimmed) hover:text-(--ui-text) md:invisible md:group-hover:visible data-[state=open]:visible" :aria-label="t('chat.more')" @click.stop>
            <UIcon name="i-lucide-ellipsis" class="size-4" />
          </button>
        </UDropdownMenu>
      </div>
    </div>
  </div>
</template>
