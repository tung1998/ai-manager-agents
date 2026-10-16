<script setup lang="ts">
// The board's grid (like a phone's home screen): square cells, 12 columns on a
// wide screen, fewer on a narrow one (a packed copy, 4 on a phone). On the full
// grid a box is moved by its grip and resized by its corner, within its
// widget's limits; the others make room and float up.
import type { WatchItem } from '~/composables/useWatchLayout'

defineProps<{ projects: Project[] }>()
const { t } = useLang()
const { items, setLayout } = useWatchLayout()

const GAP = 8
const el = ref<HTMLElement | null>(null)
const width = ref(0)
let ro: ResizeObserver | undefined
onMounted(() => {
  ro = new ResizeObserver(([e]) => { if (e) width.value = e.contentRect.width })
  if (!el.value) return
  // measured now: the observer reports on the next frame (never, in a tab not painting), the first frame packed to 4 columns
  width.value = el.value.clientWidth
  ro.observe(el.value)
})
onBeforeUnmount(() => ro?.disconnect())

// columns by width: cells stay between ~60 and ~110px
const cols = computed(() => width.value >= 900 ? GRID_COLS : width.value >= 600 ? 8 : 4)
const editable = computed(() => cols.value === GRID_COLS)
const cell = computed(() => Math.max(48, (width.value - GAP * (cols.value - 1)) / cols.value))
// Keep the desktop board a little denser so several chat panes fit without
// making each card feel oversized. Small screens keep the roomier cell height.
const rowH = computed(() => width.value >= 900 ? Math.min(cell.value, 96) : Math.min(cell.value, 110))

// what is shown: the drag's live layout, the saved one, or its packed copy
const live = ref<WatchItem[] | null>(null)
const shown = computed(() => live.value ?? reflow(items.value, cols.value, i => widgetDef(i.widget).grid.minW))
const rows = computed(() => gridRows(shown.value))

// the cell under a point of the grid
function cellAt(px: number, py: number) {
  return { x: Math.floor(px / (cell.value + GAP)), y: Math.floor(py / (rowH.value + GAP)) }
}

const active = ref<{ id: string, mode: 'move' | 'resize' } | null>(null)
const ghost = ref<{ x: number, y: number, w: number, h: number } | null>(null)
function start(it: WatchItem, mode: 'move' | 'resize', e: PointerEvent) {
  if (!editable.value || !el.value) return
  e.preventDefault()
  const box = el.value.getBoundingClientRect()
  // not copied: place() never changes them, and a box it does not shift keeps
  // its object, so only the boxes that move re-render
  const base = items.value
  const rule = widgetDef(it.widget).grid
  // where in the box it was grabbed, so it does not jump under the pointer
  const grab = { x: e.clientX - box.left - it.x * (cell.value + GAP), y: e.clientY - box.top - it.y * (rowH.value + GAP) }
  active.value = { id: it.id, mode }
  // one step per frame (the pointer fires faster), and only when the target cell changes
  let last: PointerEvent | null = null
  let frame = 0
  let key = ''
  const move = (ev: PointerEvent) => {
    last = ev
    frame ||= requestAnimationFrame(step)
  }
  const step = () => {
    frame = 0
    const ev = last
    if (!ev || !el.value) return
    // measured again: the page may have scrolled under the drag
    const now = el.value.getBoundingClientRect()
    const px = ev.clientX - now.left
    const py = ev.clientY - now.top
    let to: { x?: number, y?: number, w?: number, h?: number }
    if (mode === 'move') {
      const c = cellAt(px - grab.x + (cell.value + GAP) / 2, py - grab.y + (rowH.value + GAP) / 2)
      to = { x: c.x, y: c.y }
    } else {
      const c = cellAt(px, py)
      to = clampSize(c.x - it.x + 1, c.y - it.y + 1, rule)
    }
    const k = `${to.x},${to.y},${to.w},${to.h}`
    if (k === key) return
    key = k
    live.value = place(base, it.id, to)
    const r = live.value.find(i => i.id === it.id)!
    ghost.value = { x: r.x, y: r.y, w: r.w, h: r.h }
  }
  const up = () => {
    cancelAnimationFrame(frame)
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
    if (live.value) setLayout(live.value)
    live.value = null
    ghost.value = null
    active.value = null
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
}

// placed by transform, not left/top: moving a box then needs no layout
const style = (r: { x: number, y: number, w: number, h: number }) => ({
  transform: `translate(${r.x * (cell.value + GAP)}px, ${r.y * (rowH.value + GAP)}px)`,
  width: `${r.w * cell.value + (r.w - 1) * GAP}px`,
  height: `${r.h * rowH.value + (r.h - 1) * GAP}px`
})
</script>

<template>
  <div class="min-h-0 overflow-y-auto overflow-x-hidden">
    <div ref="el" class="relative" :class="active && 'select-none'" :style="{ height: `${Math.max(rows, 1) * (rowH + GAP)}px` }">
      <!-- the cells, while a box is dragged -->
      <div
        v-if="active" class="pointer-events-none absolute inset-0 opacity-60"
        :style="{ backgroundImage: `linear-gradient(to right, var(--ui-border) 1px, transparent 1px), linear-gradient(to bottom, var(--ui-border) 1px, transparent 1px)`, backgroundSize: `${cell + GAP}px ${rowH + GAP}px` }"
      />
      <div v-if="ghost" class="pointer-events-none absolute left-0 top-0 rounded-lg border-2 border-dashed border-(--ui-primary) bg-(--ui-primary)/10 transition-all duration-100" :style="style(ghost)" />
      <div
        v-for="it in shown" :key="it.id" class="group/box absolute left-0 top-0"
        :class="[active?.id === it.id ? 'z-20 opacity-80' : 'transition-transform duration-200', active && active.id !== it.id && 'pointer-events-none']"
        :style="style(it)"
      >
        <WatchBox :item="it" :projects="projects" :movable="editable" class="size-full" @grip="(e: PointerEvent) => start(it, 'move', e)" />
        <!-- the corner that resizes -->
        <button
          v-if="editable && !fixedSize(widgetDef(it.widget).grid)" type="button" :aria-label="t('watchW.resize')" :title="t('watchW.resize')"
          class="absolute -end-0.5 -bottom-0.5 z-10 flex size-5 cursor-se-resize touch-none items-end justify-end p-1 opacity-0 transition group-hover/box:opacity-100"
          :class="active?.id === it.id && 'opacity-100'"
          @pointerdown="start(it, 'resize', $event)"
        >
          <span class="size-2.5 rounded-br-sm border-e-2 border-b-2 border-(--ui-text-dimmed)" />
        </button>
      </div>
    </div>
  </div>
</template>
