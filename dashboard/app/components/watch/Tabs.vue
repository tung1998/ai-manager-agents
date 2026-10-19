<script setup lang="ts">
// The open dashboard's name and its ⚙ (the list and + live under "Theo dõi" in
// the sidebar); on the right, a box of any widget added to the open board.
import type { WidgetKind } from '~/utils/watchWidgets'

const props = defineProps<{ projects: Project[] }>()
const { t } = useLang()
const { boards, board, add, saveState, save } = useWatchLayout()
const settingsOpen = ref(false)
const catalogOpen = ref(false)
const index = computed(() => Math.max(0, boards.value.findIndex(b => b.id === board.value?.id)))
// changes save on their own too; the button saves now and shows whether it did
const saveLabel = computed(() => t(`watchW.save.${saveState.value}`))
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
    <UButton size="sm" color="neutral" variant="soft" icon="i-lucide-layout-panel-left" :label="t('watchW.addBox')" class="shrink-0" @click="catalogOpen = true" />
    <UButton
      size="sm" :color="saveState === 'error' ? 'error' : 'neutral'" :variant="saveState === 'saved' ? 'ghost' : 'soft'"
      :icon="saveState === 'saved' ? 'i-lucide-check' : 'i-lucide-save'" :label="saveLabel" :title="saveLabel"
      :loading="saveState === 'saving'" :disabled="saveState === 'saved'" class="shrink-0" @click="save()"
    />
  </div>
  <WatchWidgetCatalog v-model:open="catalogOpen" :projects="projects" @select="addBox" />
</template>
