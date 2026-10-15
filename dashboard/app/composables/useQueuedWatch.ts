// A card approved while an earlier one waited is queued: it runs in its turn
// on the server. While it is, its card looks again every few seconds.
export function useQueuedWatch(queued: () => boolean, look: () => Promise<void>, every = 4000) {
  let timer: ReturnType<typeof setInterval> | undefined
  const stop = () => {
    if (timer) clearInterval(timer)
    timer = undefined
  }
  watch(queued, (on) => {
    stop()
    if (on && import.meta.client) timer = setInterval(() => { look().catch(() => {}) }, every)
  }, { immediate: true })
  onBeforeUnmount(stop)
}
