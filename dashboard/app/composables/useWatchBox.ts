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
  config: Ref<Record<string, unknown>>
  set: (key: string, value: unknown) => void
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
