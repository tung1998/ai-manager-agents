<script setup lang="ts" generic="T extends { id: string, title: string, status: string, cost_usd: number, created_at: string, source?: Source }">
import type { DropdownMenuItem } from '@nuxt/ui'

// The tasks of a project, newest first, laid out like the chats: the side
// column on a wide screen, a drawer on a phone.
defineProps<{ tasks: T[], currentId?: string, menu: (t: T) => DropdownMenuItem[][], badge: (t: T) => { label: string, color: 'info' | 'success' | 'error' | 'neutral' | 'warning' } }>()
const origin = defineModel<'all' | Source>('origin', { default: 'all' })
const emit = defineEmits<{ open: [string], new: [] }>()
const { t, dateLocale } = useLang()
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="space-y-1.5 border-b border-(--ui-border) p-2">
      <UButton icon="i-lucide-plus" :label="t('task.new')" size="sm" color="neutral" variant="ghost" block class="justify-start" @click="emit('new')" />
      <SourceFilter v-model="origin" />
    </div>
    <div class="flex-1 overflow-y-auto p-1">
      <p v-if="!tasks.length" class="p-3 text-xs text-(--ui-text-muted)">{{ t('task.none') }}</p>
      <div
        v-for="x in tasks" :key="x.id"
        class="group flex cursor-pointer items-start gap-1.5 rounded-md px-2 py-1.5 text-sm"
        :class="currentId === x.id ? 'bg-(--ui-bg-accented)' : 'hover:bg-(--ui-bg-muted)'"
        @click="emit('open', x.id)"
      >
        <UIcon
          :name="x.status === 'running' ? 'i-lucide-loader-circle' : x.source && x.source !== 'web' ? sourceIcon[x.source] : 'i-lucide-list-todo'"
          class="mt-0.5 size-4 shrink-0 text-(--ui-text-muted)" :class="{ 'animate-spin': x.status === 'running' }"
        />
        <div class="min-w-0 flex-1">
          <p class="truncate">{{ x.title }}</p>
          <p class="flex items-center gap-1.5 truncate text-xs text-(--ui-text-muted)">
            <UBadge :label="badge(x).label" :color="badge(x).color" variant="subtle" size="sm" />
            <span class="truncate">{{ when(x.created_at) }}<template v-if="x.cost_usd"> · ${{ x.cost_usd.toFixed(3) }}</template></span>
          </p>
        </div>
        <UDropdownMenu :items="menu(x)" :content="{ align: 'end' }">
          <button type="button" class="-me-1 rounded px-0.5 text-(--ui-text-dimmed) hover:text-(--ui-text) md:invisible md:group-hover:visible data-[state=open]:visible" :aria-label="t('chat.more')" @click.stop>
            <UIcon name="i-lucide-ellipsis" class="size-4" />
          </button>
        </UDropdownMenu>
      </div>
    </div>
  </div>
</template>
