<script setup lang="ts">
// The open dashboard's name and its ⚙ (the list and + live under "Theo dõi" in
// the sidebar); on the right, a box of any widget added to the open board.
import type { WidgetKind } from '~/utils/watchWidgets'

defineProps<{ projects: Project[] }>()
const { t } = useLang()
const { isAdmin } = useAuth()
const { boards, board, add } = useWatchLayout()
const settingsOpen = ref(false)
const index = computed(() => Math.max(0, boards.value.findIndex(b => b.id === board.value?.id)))

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
    <div v-if="board" class="flex min-w-0 flex-1 items-center gap-0.5">
      <span class="truncate px-2 text-sm font-medium text-(--ui-text-highlighted)">{{ board.name || t('watchW.board.default', { n: index + 1 }) }}</span>
      <UPopover v-model:open="settingsOpen" :content="{ align: 'start' }">
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-settings-2" :aria-label="t('watchW.board.settings')" :title="t('watchW.board.settings')" />
        <template #content><WatchBoardSettings :board="board" :index="index" :projects="projects" @done="settingsOpen = false" /></template>
      </UPopover>
    </div>
    <UDropdownMenu :items="addMenu" :content="{ align: 'end' }" :ui="{ content: 'w-52 max-h-[70vh]' }">
      <UButton size="sm" color="neutral" variant="soft" icon="i-lucide-layout-panel-left" :label="t('watchW.addBox')" class="shrink-0" />
    </UDropdownMenu>
  </div>
</template>
