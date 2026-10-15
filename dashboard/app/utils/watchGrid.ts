// The watch board's grid (like a phone's home screen): boxes take whole cells
// (1×1, 2×2, 4×4…), never overlap, and float up to fill the gaps. Pure
// functions over plain rects, so they are tested on their own
// (scripts/test-watch-grid.ts).

export const GRID_COLS = 12 // the layout is kept for this width; narrower screens get a packed copy

export interface Rect { id: string, x: number, y: number, w: number, h: number }
export interface SizeRule { w: number, h: number, minW: number, minH: number, maxW: number, maxH: number }

const hit = (a: Rect, b: Rect) => a.id !== b.id && a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h

// a size within a widget's limits and the grid's width
export function clampSize(w: number, h: number, rule: SizeRule, cols = GRID_COLS) {
  return {
    w: Math.max(Math.min(rule.minW, cols), Math.min(rule.maxW, cols, Math.round(w))),
    h: Math.max(rule.minH, Math.min(rule.maxH, Math.round(h)))
  }
}

// the lowest free row for r at its column (nothing in `placed` under it)
function drop(r: Rect, placed: Rect[]): Rect {
  const out = { ...r, y: 0 }
  for (;;) {
    const c = placed.find(p => hit(out, p))
    if (!c) return out
    out.y = c.y + c.h
  }
}

// every box floated up as far as it goes, in reading order; `fixed` stays
// where it was put (the one being dragged), the others make room around it
export function compact<T extends Rect>(items: T[], fixed?: string): T[] {
  const placed: Rect[] = []
  const pinned = fixed ? items.find(i => i.id === fixed) : undefined
  if (pinned) placed.push(pinned)
  const out = new Map<string, T>()
  if (pinned) out.set(pinned.id, pinned)
  const rest = items.filter(i => i !== pinned).sort((a, b) => a.y - b.y || a.x - b.x)
  for (const it of rest) {
    const r = { ...it, ...drop(it, placed) }
    placed.push(r)
    out.set(it.id, r)
  }
  return items.map(i => out.get(i.id)!)
}

// one box moved and/or resized: kept in the grid, the others float around it
export function place<T extends Rect>(items: T[], id: string, to: { x?: number, y?: number, w?: number, h?: number }, cols = GRID_COLS): T[] {
  const moved = items.map((i) => {
    if (i.id !== id) return i
    const w = Math.min(cols, to.w ?? i.w)
    return { ...i, w, h: to.h ?? i.h, x: Math.max(0, Math.min(cols - w, to.x ?? i.x)), y: Math.max(0, to.y ?? i.y) }
  })
  return compact(moved, id)
}

// where a new box of w×h goes: the first free spot reading left to right, top to bottom
export function firstFit(items: Rect[], w: number, h: number, cols = GRID_COLS): { x: number, y: number } {
  w = Math.min(w, cols)
  for (let y = 0; ; y++) {
    for (let x = 0; x + w <= cols; x++) {
      const r = { id: '\0', x, y, w, h }
      if (!items.some(i => hit(r, i))) return { x, y }
    }
  }
}

// the board on a narrower screen (a phone: 4 columns): same order, each box
// as wide as it can be up to its own width, packed into the first free spot
export function reflow<T extends Rect>(items: T[], cols: number, minW: (it: T) => number = () => 1): T[] {
  if (cols >= GRID_COLS) return items
  const placed: T[] = []
  for (const it of [...items].sort((a, b) => a.y - b.y || a.x - b.x)) {
    const w = Math.max(Math.min(minW(it), cols), Math.min(it.w, cols))
    placed.push({ ...it, w, ...firstFit(placed, w, it.h, cols) })
  }
  return items.map(i => placed.find(p => p.id === i.id)!)
}

export const gridRows = (items: Rect[]) => items.reduce((a, i) => Math.max(a, i.y + i.h), 0)
