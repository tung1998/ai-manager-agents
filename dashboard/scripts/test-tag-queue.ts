// Regression tests for the tag-save queue used by ChatPanel.vue saveTags():
// rapid tag edits must not lose an earlier removal/add to a later request
// that resolves first. These import the real app/utils/tagQueue.ts (not a
// reimplementation), so a change to that file is what these tests exercise.
import assert from 'node:assert/strict'
import { applyTagDiff, queueTagOp, type TagChains } from '../app/utils/tagQueue.ts'

function delay<T>(ms: number, value: T): Promise<T> {
  return new Promise(r => setTimeout(() => r(value), ms))
}

// (a) chip remove via ChatTags, then a picker add, while the first PUT is
// still in flight. Reproduces the exact scenario ChatPanel.vue must avoid:
// current.tags is ['A','B']; request #1 (remove A) is slow; before it
// resolves, a picker add of 'C' is wired up. 'eager' captures `before` (the
// tags the picker started from) synchronously at emit time, the way
// ChatPanel.vue's @update:model-value handlers do (`const before = c.tags`,
// read once, before `saveTags` is even called). 'lazy' instead re-reads
// current.tags from inside the queued op — i.e. after request #1 may already
// have resolved and overwritten it — which is the bug this piece fixes.
async function simulate(diffBefore: 'eager' | 'lazy') {
  const chains: TagChains = new Map()
  const current: { value: { id: string, tags: string[] } | null } = { value: { id: 'c1', tags: ['A', 'B'] } }
  let calls = 0
  const putTags = async (tags: string[]) => {
    calls++
    if (calls === 1) await delay(20, null) // request #1 (remove A) is the slow one
    return tags
  }
  async function saveTags(c: { id: string, tags: string[] }, mutate: (tags: string[]) => string[]) {
    await queueTagOp(chains, c.id, c.tags ?? [], async (base) => {
      const tags = mutate(base)
      const saved = await putTags(tags)
      if (current.value?.id === c.id) current.value = { ...current.value, tags: saved }
      return saved
    })
  }

  const c = current.value!
  void saveTags(c, base => base.filter(t => t !== 'A')) // request #1: remove A

  const after = [...(current.value!.tags ?? []), 'C']
  if (diffBefore === 'eager') {
    const before = current.value!.tags ?? [] // captured now, like the real fix
    void saveTags(current.value!, base => applyTagDiff(base, before, after))
  } else {
    // the bug: `before` is re-read lazily once the op actually runs, by
    // which point request #1 may have already resolved and changed it
    void saveTags(current.value!, base => applyTagDiff(base, current.value!.tags ?? [], after))
  }

  await delay(30, null)
  return current.value!.tags
}

{
  const fixed = await simulate('eager')
  assert.deepEqual(fixed.sort(), ['B', 'C'], 'capturing `before` at emit time: both the removal of A and the addition of C survive')
}
{
  const buggy = await simulate('lazy')
  assert.ok(buggy.includes('A'), 'reading `before` lazily inside the op: A reappears (the bug this piece fixes)')
}

// (b) once a chain finishes, its entry must not linger: a later save must
// start from the caller's own current tags (e.g. just updated by an SSE
// 'conversation' event from another tab), not from a stale queued promise
{
  const chains: TagChains = new Map()
  await queueTagOp(chains, 'c1', ['A'], async base => base) // chain finishes and clears
  assert.equal(chains.has('c1'), false, 'a finished chain is removed so the next save re-bases on fresh tags')

  const externallyUpdated = ['A', 'EXTERNAL'] // arrived via SSE after the chain above finished
  const saved = await queueTagOp(chains, 'c1', externallyUpdated, async base => base.filter(t => t !== 'A'))
  assert.deepEqual(saved, ['EXTERNAL'], 'the externally-added tag is kept, not overwritten by a stale base')
}

// (c) a failed PUT (error after an await) must not break the chain: the next
// save still runs, built from the same base the failed one saw. ChatPanel's
// real op always catches internally and returns `base` on failure — queueTagOp
// itself doesn't catch, so an op that rejects would reject every save queued
// after it
{
  const chains: TagChains = new Map()
  const p1 = queueTagOp(chains, 'c1', ['A', 'B'], async (base) => {
    try {
      await delay(5, null)
      throw new Error('network error') // simulates the PUT failing
    } catch {
      return base
    }
  })
  const p2 = queueTagOp(chains, 'c1', ['A', 'B'], async (base) => base.filter(t => t !== 'B'))
  const [first, second] = await Promise.all([p1, p2])
  assert.deepEqual(first, ['A', 'B'], 'the failed save resolves with its own base, not a rejection')
  assert.deepEqual(second, ['A'], 'a save after a failed one still runs and builds on the right base')
}

// (d) an op whose own work throws synchronously, before any await (e.g. a
// mutator that throws) — this is exactly why ChatPanel.vue's saveTags must
// call `mutate(base)` inside its try, not before it
{
  const chains: TagChains = new Map()
  const p1 = queueTagOp(chains, 'c1', ['A', 'B'], async (base) => {
    try {
      const tags = ((): string[] => { throw new Error('mutate blew up') })()
      return await delay(0, tags)
    } catch {
      return base
    }
  })
  const p2 = queueTagOp(chains, 'c1', ['A', 'B'], async (base) => base.filter(t => t !== 'B'))
  const [first, second] = await Promise.all([p1, p2])
  assert.deepEqual(first, ['A', 'B'], 'a synchronous throw inside the op resolves with its own base, not a rejection')
  assert.deepEqual(second, ['A'], 'a save after a synchronously-throwing one still runs and builds on the right base')
}

console.log('test-tag-queue: ok')
