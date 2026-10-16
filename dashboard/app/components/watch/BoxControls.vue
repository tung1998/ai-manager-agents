<script setup lang="ts">
// A box's buttons. lead: its grip (drag to move), what it shows (widget), its
// project; actions: ⚙ and a menu (size, copy, close). A tight box (a small
// one) folds its project into the menu so its title keeps room.
import type { WatchItem } from '~/composables/useWatchLayout'
import type { WidgetKind } from '~/utils/watchWidgets'

const props = defineProps<{ item: WatchItem, projects: Project[], tight?: boolean, movable?: boolean, part: 'lead' | 'actions' }>()
const emit = defineEmits<{ grip: [e: PointerEvent] }>()
const { t } = useLang()
const { close, duplicate, setPane, setSize, board } = useWatchLayout()
const def = computed(() => widgetDef(props.item.widget))
const project = computed(() => props.projects.find(p => p.id === props.item.projectId))
const btn = computed(() => props.tight ? 'xs' : 'sm')

// what it shows is picked in the same catalog as a box added (with its samples)
const catalogOpen = ref(false)
// another widget: its own settings start over, the project stays (or the board's)
function change(k: WidgetKind) {
  if (k === props.item.widget) return
  setPane(props.item.id, { widget: k, config: undefined, conversationId: undefined, projectId: props.item.projectId || board.value?.projectId || '' })
}
const projectItems = computed(() => [
  ...(def.value.project === 'optional' ? [{ label: t('watchW.allProjects'), icon: !props.item.projectId ? 'i-lucide-check' : 'i-lucide-layers', onSelect: () => setPane(props.item.id, { projectId: '', conversationId: undefined }) }] : []),
  ...props.projects.map(p => ({
    label: p.name,
    icon: p.id === props.item.projectId ? 'i-lucide-check' : (p.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder'),
    onSelect: () => { if (p.id !== props.item.projectId) setPane(props.item.id, { projectId: p.id, conversationId: undefined, config: props.item.widget === 'chat' ? undefined : { ...props.item.config, processId: '', service: '', monitorId: '' } }) }
  }))
])
const projectLabel = computed(() => project.value?.name ?? (def.value.project === 'optional' ? t('watchW.allProjects') : t('watch.project')))
const sizeItems = computed(() => sizePresets(def.value.grid).map(([w, h]) => ({
  label: `${w} × ${h}`,
  icon: w === props.item.w && h === props.item.h ? 'i-lucide-check' : 'i-lucide-square-dashed',
  onSelect: () => setSize(props.item.id, w, h)
})))
const moreMenu = computed(() => [
  [
    ...(props.tight ? [{ label: t('watchW.changeWidget'), icon: def.value.icon, onSelect: () => { catalogOpen.value = true } }] : []),
    ...(props.tight && def.value.project !== 'none' ? [{ label: projectLabel.value, icon: 'i-lucide-folder', children: [projectItems.value] }] : []),
    ...(fixedSize(def.value.grid) ? [] : [{ label: t('watchW.sizeNow', { w: props.item.w, h: props.item.h }), icon: 'i-lucide-scaling', children: [sizeItems.value] }])
  ],
  [
    { label: t('watchW.duplicate'), icon: 'i-lucide-copy', onSelect: () => duplicate(props.item.id) },
    { label: t('watch.close'), icon: 'i-lucide-x', color: 'error' as const, onSelect: () => close(props.item.id) }
  ]
])
</script>

<template>
  <div v-if="part === 'lead'" class="flex min-w-0 shrink-0 items-center gap-0.5">
    <span
      v-if="movable" class="cursor-grab touch-none px-0.5 text-(--ui-text-dimmed) hover:text-(--ui-text) active:cursor-grabbing" :title="t('watchW.drag')"
      @pointerdown="emit('grip', $event)"
    >
      <UIcon name="i-lucide-grip-vertical" class="size-4" />
    </span>
    <UButton v-if="!tight" :size="btn" color="neutral" variant="ghost" :icon="def.icon" :aria-label="t('watchW.changeWidget')" :title="t('watchW.changeWidget')" @click="catalogOpen = true" />
    <UIcon v-else :name="def.icon" class="mx-0.5 size-3.5 shrink-0 text-(--ui-text-muted)" />
    <UDropdownMenu v-if="def.project !== 'none' && !tight" :items="[projectItems]" :content="{ align: 'start' }" :ui="{ content: 'max-h-80 w-60' }">
      <UButton
        :size="btn" color="neutral" variant="soft" :icon="project ? (project.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder') : 'i-lucide-layers'"
        :label="projectLabel" :title="t('watch.project')" class="max-w-36" :ui="{ label: 'truncate' }"
      />
    </UDropdownMenu>
    <WatchWidgetCatalog v-if="catalogOpen" v-model:open="catalogOpen" :projects="projects" :current="item.widget" @select="change" />
  </div>
  <div v-else class="flex shrink-0 items-center">
    <UPopover :content="{ align: 'end' }">
      <UButton :size="btn" color="neutral" variant="ghost" icon="i-lucide-settings-2" :aria-label="t('watchW.settings')" :title="t('watchW.settings')" />
      <template #content><WatchBoxSettings :item="item" /></template>
    </UPopover>
    <UDropdownMenu :items="moreMenu" :content="{ align: 'end' }" :ui="{ content: 'w-52' }">
      <UButton :size="btn" color="neutral" variant="ghost" icon="i-lucide-ellipsis-vertical" :aria-label="t('watchW.more')" :title="t('watchW.more')" />
    </UDropdownMenu>
    <WatchWidgetCatalog v-if="catalogOpen" v-model:open="catalogOpen" :projects="projects" :current="item.widget" @select="change" />
  </div>
</template>
