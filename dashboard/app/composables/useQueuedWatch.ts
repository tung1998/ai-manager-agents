// A card approved while an earlier one waited is queued: it runs in its turn
// on the server. While it is, its card looks again when cards change.
export function useQueuedWatch(queued: () => boolean, look: () => Promise<void>) {
  useLive(['actions', 'patches'], () => {
    if (queued()) look().catch(() => {})
  })
}
