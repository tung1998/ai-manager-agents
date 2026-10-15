// What a watch box hands its widget: its size (widgets lay out by the box,
// not the window, so a drag of the divider reflows them) and its settings.
// Widgets reload on their own when their data changes (useLiveFetch).
import type { InjectionKey, Ref } from 'vue'
import type { WidgetKind } from '~/utils/watchWidgets'

export interface WatchBoxCtx {
  paneId: string
  widget: Ref<WidgetKind>
  projectId: Ref<string>
  size: Ref<{ w: number, h: number }>
  cells: Ref<{ w: number, h: number }> // its room in grid cells
  config: Ref<Record<string, unknown>>
  set: (key: string, value: unknown) => void
}

export const watchBoxKey: InjectionKey<WatchBoxCtx> = Symbol('watch-box')

export function useWatchBox() {
  const ctx = inject(watchBoxKey)
  if (!ctx) throw new Error('useWatchBox outside a watch box')
  const { size, cells } = ctx
  // the box's shape, by its own width/height
  const narrow = computed(() => size.value.w < 340)
  const tiny = computed(() => size.value.w < 220 || size.value.h < 130)
  const short = computed(() => size.value.h < 260)
  // how a widget shows itself, by its cells (like a phone's widgets): a badge
  // (1×1), a strip (one row), a column (one cell wide), a card (up to 2×2… 3×2),
  // a panel (larger)
  const shape = computed<WatchShape>(() => {
    const { w, h } = cells.value
    if (w === 1 && h === 1) return 'badge'
    if (h === 1) return 'strip'
    if (w === 1) return 'column'
    return w * h <= 6 ? 'card' : 'panel'
  })
  return { ...ctx, narrow, tiny, short, shape }
}
export type WatchShape = 'badge' | 'strip' | 'column' | 'card' | 'panel'

// a clock for countdowns (no request): ticks every `ms` while the box is shown
export function useNow(ms = 1000) {
  const now = ref(Date.now())
  let timer: ReturnType<typeof setInterval> | undefined
  onMounted(() => { timer = setInterval(() => { now.value = Date.now() }, ms) })
  onBeforeUnmount(() => clearInterval(timer))
  return now
}

// time left, short: 45s, 12m, 2h 5m (one unit: 2h), 3d
export function fmtLeft(ms: number, one = false) {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return one ? `${Math.floor(s / 3600)}h` : `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
  return `${Math.floor(s / 86400)}d`
}

// runs fn every `seconds` (0: never), while the box is shown: for what the
// live stream does not tell (git, machine metrics…)
export function useWatchPoll(fn: () => unknown, seconds: MaybeRefOrGetter<number>) {
  let timer: ReturnType<typeof setInterval> | undefined
  const start = () => {
    clearInterval(timer)
    const s = toValue(seconds)
    if (s > 0) timer = setInterval(fn, s * 1000)
  }
  onMounted(() => watch(() => toValue(seconds), start, { immediate: true }))
  onBeforeUnmount(() => clearInterval(timer))
}
