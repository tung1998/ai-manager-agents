// Pure tag-save queueing, shared by ChatPanel.vue and its tests (no Vue, no
// fetch): a chat's tags are a full-replace PUT, so two in-flight saves for
// the same conversation must run one after another, each built from the
// previous one's result — never from a stale snapshot taken before it.
import { sameTag } from './tags.ts'

export type TagChains = Map<string, Promise<string[]>>

// Runs `op` for `id` after any save already queued for it resolves, feeding
// it that save's result (or `fallback` if nothing is queued yet). The entry
// is removed once this is the last queued op, so the next unrelated call
// starts from the caller's own latest known tags (e.g. one just arrived over
// SSE) instead of a promise this queue would otherwise hold onto forever.
export function queueTagOp(chains: TagChains, id: string, fallback: string[], op: (base: string[]) => Promise<string[]>): Promise<string[]> {
  const prev = chains.get(id) ?? Promise.resolve(fallback)
  let next: Promise<string[]>
  next = prev.then(op).finally(() => {
    if (chains.get(id) === next) chains.delete(id)
  })
  chains.set(id, next)
  return next
}

// What changed between the tags a picker started from and what it emitted,
// applied onto `base` (the queue's latest known tags) instead of onto
// `before` directly — so a concurrent change to another tag on the same
// chat isn't clobbered by this one re-sending the whole array.
export function applyTagDiff(base: string[], before: string[], after: string[]): string[] {
  const removed = before.filter(b => !after.some(a => sameTag(a, b)))
  const added = after.filter(a => !before.some(b => sameTag(a, b)))
  const out = base.filter(t => !removed.some(r => sameTag(r, t)))
  for (const a of added) if (!out.some(t => sameTag(t, a))) out.push(a)
  return out
}
