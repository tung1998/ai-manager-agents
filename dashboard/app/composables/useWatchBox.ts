// What a watch box hands its widget: its size (widgets lay out by the box,
// not the window, so a drag of the divider reflows them), its settings and the
// dashboard's refresh period.
import type { InjectionKey, Ref } from 'vue'
import type { WidgetKind } from '~/utils/watchWidgets'

export interface WatchBoxCtx {
  paneId: string
  widget: Ref<WidgetKind>
  projectId: Ref<string>
  size: Ref<{ w: number, h: number }>
  config: Ref<Record<string, unknown>>
  set: (key: string, value: unknown) => void
  refresh: Ref<number> // seconds, 0: only on change
}

export const watchBoxKey: InjectionKey<WatchBoxCtx> = Symbol('watch-box')

export function useWatchBox() {
  const ctx = inject(watchBoxKey)
  if (!ctx) throw new Error('useWatchBox outside a watch box')
  const { size } = ctx
  // the box's shape, by its own width/height
  const narrow = computed(() => size.value.w < 340)
  const tiny = computed(() => size.value.w < 220 || size.value.h < 130)
  const short = computed(() => size.value.h < 260)
  return { ...ctx, narrow, tiny, short }
}

// runs fn every `seconds` (or the dashboard's period), while the box is shown
export function useWatchPoll(fn: () => unknown, seconds?: MaybeRefOrGetter<number>) {
  const { refresh } = useWatchBox()
  let timer: ReturnType<typeof setInterval> | undefined
  const start = () => {
    clearInterval(timer)
    const s = toValue(seconds) || refresh.value
    if (s > 0) timer = setInterval(fn, s * 1000)
  }
  onMounted(() => watch([() => toValue(seconds), refresh], start, { immediate: true }))
  onBeforeUnmount(() => clearInterval(timer))
}
