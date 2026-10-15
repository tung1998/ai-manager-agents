<script setup lang="ts">
// The dashboards as tabs (like Graylog's pages): pick one, ⚙ on the open one,
// + for a new one; on the right, a box of any widget added to the open board.
import type { WidgetKind } from '~/utils/watchWidgets'

defineProps<{ projects: Project[] }>()
const { t } = useLang()
const { isAdmin } = useAuth()
const { boards, board, selectBoard, addBoard, add } = useWatchLayout()
const settingsOpen = ref(false)
const boardName = (i: number) => boards.value[i]?.name || t('watchW.board.default', { n: i + 1 })

// grouped by size, a title over each group
const addMenu = computed(() => (['sm', 'md', 'lg'] as const).map(size => [
  { label: t(`watchW.group.${size}`), type: 'label' as const },
  ...widgetDefs.filter(w => widgetGroup(w) === size && (!w.admin || isAdmin.value)).map(w => ({
    label: t(`watchW.kind.${w.kind}` as 'watchW.kind.chat'),
    icon: w.icon,
    onSelect: () => addBox(w.kind)
  }))
]))
// a new box starts on the board's project (none: it asks for one if it needs it)
const addBox = (k: WidgetKind) => add(board.value?.projectId ?? '', undefined, k)
</script>

<template>
  <div class="flex min-w-0 items-center gap-1">
    <div class="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto" role="tablist">
      <div
        v-for="(b, i) in boards" :key="b.id" role="tab" :aria-selected="b.id === board?.id"
        class="flex shrink-0 items-center rounded-md transition"
        :class="b.id === board?.id ? 'bg-(--ui-bg-elevated) text-(--ui-text-highlighted)' : 'text-(--ui-text-muted) hover:bg-(--ui-bg-elevated)/50'"
      >
        <button type="button" class="max-w-48 truncate px-3 py-1.5 text-sm font-medium" @click="selectBoard(b.id)">{{ boardName(i) }}</button>
        <UPopover v-if="b.id === board?.id" v-model:open="settingsOpen" :content="{ align: 'start' }">
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-settings-2" class="me-1" :aria-label="t('watchW.board.settings')" :title="t('watchW.board.settings')" />
          <template #content><WatchBoardSettings :board="b" :index="i" :projects="projects" @done="settingsOpen = false" /></template>
        </UPopover>
      </div>
      <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-plus" :aria-label="t('watchW.board.add')" :title="t('watchW.board.add')" @click="addBoard" />
    </div>
    <UDropdownMenu :items="addMenu" :content="{ align: 'end' }" :ui="{ content: 'w-52 max-h-[70vh]' }">
      <UButton size="sm" color="neutral" variant="soft" icon="i-lucide-layout-panel-left" :label="t('watchW.addBox')" class="shrink-0" />
    </UDropdownMenu>
  </div>
</template>
