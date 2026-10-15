// Tests for the watch board's grid (app/utils/watchGrid.ts): boxes never
// overlap, float up, a dragged box keeps its spot, sizes stay in their
// widget's limits, a phone gets a packed copy.
import assert from 'node:assert/strict'
import { clampSize, compact, firstFit, place, reflow, type Rect } from '../app/utils/watchGrid.ts'

const overlap = (a: Rect, b: Rect) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h
function noOverlap(items: Rect[]) {
  for (let i = 0; i < items.length; i++) {
    for (let j = i + 1; j < items.length; j++) assert.ok(!overlap(items[i]!, items[j]!), `${items[i]!.id} overlaps ${items[j]!.id}`)
  }
}
const r = (id: string, x: number, y: number, w: number, h: number): Rect => ({ id, x, y, w, h })

// float up: a gap above closes
{
  const out = compact([r('a', 0, 3, 2, 2)])
  assert.deepEqual(out[0], r('a', 0, 0, 2, 2))
}

// first free spot: beside, then below
{
  const items = [r('a', 0, 0, 6, 2)]
  assert.deepEqual(firstFit(items, 6, 2), { x: 6, y: 0 })
  assert.deepEqual(firstFit([...items, r('b', 6, 0, 6, 2)], 4, 4), { x: 0, y: 2 })
}

// dragging a box down onto another: they trade rows, nothing overlaps
{
  const items = [r('a', 0, 0, 4, 2), r('b', 0, 2, 4, 2)]
  const out = place(items, 'a', { y: 2 })
  noOverlap(out)
  assert.equal(out.find(i => i.id === 'a')!.y, 2)
  assert.equal(out.find(i => i.id === 'b')!.y, 0)
}

// dragging a box up onto another: the other goes under it
{
  const items = [r('a', 0, 0, 4, 2), r('b', 0, 2, 4, 2)]
  const out = place(items, 'b', { y: 0 })
  noOverlap(out)
  assert.equal(out.find(i => i.id === 'b')!.y, 0)
  assert.equal(out.find(i => i.id === 'a')!.y, 2)
}

// growing a box pushes its neighbours down, never over it
{
  const items = [r('a', 0, 0, 2, 2), r('b', 2, 0, 2, 2), r('c', 0, 2, 4, 1)]
  const out = place(items, 'a', { w: 4, h: 3 })
  noOverlap(out)
  assert.deepEqual(out.find(i => i.id === 'a'), r('a', 0, 0, 4, 3))
}

// a box never leaves the grid
{
  const out = place([r('a', 0, 0, 4, 2)], 'a', { x: 20, y: -3 })
  assert.deepEqual(out[0], r('a', 8, 0, 4, 2))
}

// sizes within the widget's limits and the grid
{
  const rule = { w: 2, h: 2, minW: 1, minH: 1, maxW: 4, maxH: 4 }
  assert.deepEqual(clampSize(9, 0, rule), { w: 4, h: 1 })
  assert.deepEqual(clampSize(0, 9, rule), { w: 1, h: 4 })
  assert.deepEqual(clampSize(8, 2, { ...rule, maxW: 12 }, 4), { w: 4, h: 2 })
}

// a phone: 4 columns, reading order kept, wide boxes narrowed, nothing overlaps
{
  const items = [r('a', 0, 0, 8, 4), r('b', 8, 0, 2, 2), r('c', 10, 0, 2, 2), r('d', 8, 2, 4, 2)]
  const out = reflow(items, 4)
  noOverlap(out)
  for (const i of out) assert.ok(i.x + i.w <= 4)
  assert.deepEqual(out.find(i => i.id === 'a'), r('a', 0, 0, 4, 4))
  assert.deepEqual(out.find(i => i.id === 'b'), r('b', 0, 4, 2, 2))
  assert.deepEqual(out.find(i => i.id === 'c'), r('c', 2, 4, 2, 2))
}

// random moves never leave an overlap
{
  let items: Rect[] = Array.from({ length: 10 }, (_, i) => r(`n${i}`, (i * 3) % 12, Math.floor(i / 4) * 2, 1 + (i % 4), 1 + (i % 3)))
  items = compact(items)
  let seed = 7
  const rnd = (n: number) => { seed = (seed * 1103515245 + 12345) % 2147483648; return seed % n }
  for (let k = 0; k < 300; k++) {
    const it = items[rnd(items.length)]!
    items = place(items, it.id, { x: rnd(12), y: rnd(10), w: 1 + rnd(5), h: 1 + rnd(4) })
    noOverlap(items)
  }
  noOverlap(compact(items))
}

console.log('watch grid: ok')
