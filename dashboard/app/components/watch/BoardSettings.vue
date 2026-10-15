<script setup lang="ts">
// A dashboard's ⚙: its name, how often its widgets reload, the project new
// boxes start on; copy or delete it.
import type { WatchBoard } from '~/composables/useWatchLayout'

const props = defineProps<{ board: WatchBoard, index: number, projects: Project[] }>()
const emit = defineEmits<{ done: [] }>()
const { t } = useLang()
const { updateBoard, duplicateBoard, removeBoard, boards, canAddBoard } = useWatchLayout()
const NONE = '__none'
const refreshItems = computed(() => [0, 15, 30, 60, 300].map(s => ({ value: s, label: s ? t('watchW.board.every', { s: s < 60 ? `${s}s` : `${s / 60}m` }) : t('watchW.board.onChange') })))
const projectItems = computed(() => [{ value: NONE, label: t('watchW.board.noProject') }, ...props.projects.map(p => ({ value: p.id, label: p.name }))])
function remove() {
  if (props.board.items.length && !confirm(t('watchW.board.confirmDelete'))) return
  removeBoard(props.board.id)
  emit('done')
}
</script>

<template>
  <div class="w-72 space-y-3 p-3">
    <UFormField :label="t('common.name')">
      <UInput :model-value="board.name" :placeholder="t('watchW.board.default', { n: index + 1 })" class="w-full" @update:model-value="(v) => updateBoard(board.id, { name: String(v) })" />
    </UFormField>
    <UFormField :label="t('watchW.board.refresh')" :hint="undefined">
      <USelect :model-value="board.refresh" :items="refreshItems" class="w-full" @update:model-value="(v) => updateBoard(board.id, { refresh: Number(v) })" />
    </UFormField>
    <UFormField :label="t('watchW.board.project')">
      <USelect :model-value="board.projectId || NONE" :items="projectItems" class="w-full" @update:model-value="(v) => updateBoard(board.id, { projectId: v === NONE ? '' : String(v) })" />
    </UFormField>
    <div class="flex gap-2 border-t border-(--ui-border) pt-3">
      <UButton size="sm" color="neutral" variant="soft" icon="i-lucide-copy" :label="t('watchW.board.duplicate')" :disabled="!canAddBoard" @click="duplicateBoard(board.id); emit('done')" />
      <UButton size="sm" color="error" variant="ghost" icon="i-lucide-trash-2" :label="t('common.delete')" :disabled="boards.length < 2 && !board.items.length" class="ms-auto" @click="remove" />
    </div>
  </div>
</template>
