// The watch screen (like Graylog's dashboards, laid out like a phone's home
// screen): several dashboards, each a grid of boxes taking whole cells (1×1,
// 2×2, 4×4…), each box a widget (a chat, a process, a monitor…). Kept in this
// browser (localStorage).
import type { WidgetKind } from '~/utils/watchWidgets'

// a box: what it shows and where (x, y, w, h in cells of the 12-column grid)
export interface WatchItem { id: string, widget: WidgetKind, projectId: string, conversationId?: string, config?: Record<string, unknown>, title?: string, x: number, y: number, w: number, h: number }
// refresh: seconds between reloads of the widgets that poll (0: only on change);
// projectId: the project a new box starts on
export interface WatchBoard { id: string, name: string, items: WatchItem[], refresh: number, projectId: string }

// boards saved before the grid: a tree of splits whose leaves were boxes
interface OldPane { kind: 'pane', widget?: WidgetKind, projectId: string, conversationId?: string, config?: Record<string, unknown>, title?: string }
type OldNode = OldPane | { kind: 'split', children: OldNode[] }

const KEY = 'office.watch.boards'
export const MAX_BOARDS = 5
const OLD_KEY = 'office.watch.layout' // one board, before dashboards
const newId = () => Math.random().toString(36).slice(2, 10)
const blank = (items: WatchItem[] = []): WatchBoard => ({ id: newId(), name: '', items, refresh: 30, projectId: '' })

// the old tree's boxes, in reading order, each at its widget's size, packed
function fromTree(root: OldNode | null): WatchItem[] {
  const panes: OldPane[] = []
  const walk = (n: OldNode) => n.kind === 'pane' ? panes.push(n) : n.children.forEach(walk)
  if (root) walk(root)
  const items: WatchItem[] = []
  for (const p of panes.filter(p => p.widget || p.projectId)) {
    const widget = p.widget ?? 'chat'
    const { w, h } = widgetDef(widget).grid
    items.push({ id: newId(), widget, projectId: p.projectId, conversationId: p.conversationId, config: p.config, title: p.title, w, h, ...firstFit(items, w, h) })
  }
  return items
}
function upgrade(b: WatchBoard & { root?: OldNode | null }): WatchBoard {
  if (Array.isArray(b.items)) return b
  const { root, ...rest } = b
  return { ...rest, items: fromTree(root ?? null) }
}

export function useWatchLayout() {
  const boards = useState<WatchBoard[]>('watch-boards', () => [])
  const activeId = useState('watch-active', () => '')
  const loaded = useState('watch-layout-loaded', () => false)

  if (import.meta.client && !loaded.value) {
    loaded.value = true
    try {
      const raw = localStorage.getItem(KEY)
      if (raw) {
        const saved = JSON.parse(raw) as { active: string, boards: WatchBoard[] }
        boards.value = saved.boards.map(upgrade)
        activeId.value = saved.active
      } else {
        const old = localStorage.getItem(OLD_KEY)
        boards.value = [blank(fromTree(old ? JSON.parse(old) as OldNode : null))]
      }
    } catch { /* broken or private mode: start empty */ }
    if (!boards.value.length) boards.value = [blank()]
    if (!boards.value.some(b => b.id === activeId.value)) activeId.value = boards.value[0]!.id
    // saved for the app's whole life: made in the first caller's scope, it
    // stopped when that page was left and was never made again (changes then
    // lived only in memory, lost on reload)
    effectScope(true).run(() => watch([boards, activeId], ([list, active]) => {
      try {
        localStorage.setItem(KEY, JSON.stringify({ active, boards: list }))
        localStorage.removeItem(OLD_KEY)
      } catch { /* private mode */ }
    }, { deep: true }))
  }

  const board = computed(() => boards.value.find(b => b.id === activeId.value) ?? boards.value[0] ?? null)
  const items = computed(() => board.value?.items ?? [])
  const item = (id: string) => items.value.find(i => i.id === id)

  // a new box at its widget's size, in the first free spot
  function add(projectId: string, conversationId?: string, widget: WidgetKind = 'chat', config?: Record<string, unknown>) {
    if (!board.value) return
    const { w, h } = widgetDef(widget).grid
    board.value.items.push({ id: newId(), widget, projectId, conversationId, config, w, h, ...firstFit(board.value.items, w, h) })
  }

  function close(id: string) {
    if (!board.value) return
    board.value.items = compact(board.value.items.filter(i => i.id !== id))
  }

  // a copy right after it (same widget, project and settings)
  function duplicate(id: string) {
    const src = item(id)
    if (!src || !board.value) return
    const copy: WatchItem = { ...JSON.parse(JSON.stringify(src)) as WatchItem, id: newId() }
    Object.assign(copy, firstFit(board.value.items, copy.w, copy.h))
    board.value.items.push(copy)
  }

  function setPane(id: string, patch: Partial<Omit<WatchItem, 'id' | 'x' | 'y' | 'w' | 'h'>>) {
    const it = item(id)
    if (!it) return
    Object.assign(it, patch)
    // another widget: its own size limits
    if (patch.widget) setSize(id, it.w, it.h)
  }

  // one setting of a box's widget (a process to follow, the console on/off…)
  function setConfig(id: string, key: string, value: unknown) {
    const it = item(id)
    if (it) it.config = { ...it.config, [key]: value }
  }

  // a size within its widget's limits; the others make room
  function setSize(id: string, w: number, h: number) {
    const it = item(id)
    if (!it || !board.value) return
    board.value.items = compact(place(board.value.items, id, clampSize(w, h, widgetDef(it.widget).grid)))
  }

  // the layout after a drag (already placed by the grid)
  function setLayout(next: WatchItem[]) {
    if (board.value) board.value.items = compact(next)
  }

  function clear() {
    if (board.value) board.value.items = []
  }

  // ---- dashboards ----
  // listed under "Theo dõi" in the sidebar: a short list, so at most 5
  const canAddBoard = computed(() => boards.value.length < MAX_BOARDS)
  function addBoard() {
    if (!canAddBoard.value) return
    const b = blank()
    boards.value.push(b)
    activeId.value = b.id
  }
  function duplicateBoard(id: string) {
    const src = boards.value.find(b => b.id === id)
    if (!src || !canAddBoard.value) return
    const copy: WatchBoard = { ...JSON.parse(JSON.stringify(src)) as WatchBoard, id: newId(), name: src.name ? `${src.name} (2)` : '' }
    copy.items.forEach((i) => { i.id = newId() })
    boards.value.splice(boards.value.indexOf(src) + 1, 0, copy)
    activeId.value = copy.id
  }
  function removeBoard(id: string) {
    const i = boards.value.findIndex(b => b.id === id)
    if (i < 0) return
    boards.value.splice(i, 1)
    if (!boards.value.length) boards.value.push(blank())
    if (activeId.value === id) activeId.value = boards.value[Math.max(0, i - 1)]!.id
  }
  function updateBoard(id: string, patch: Partial<Omit<WatchBoard, 'id' | 'items'>>) {
    const b = boards.value.find(x => x.id === id)
    if (b) Object.assign(b, patch)
  }
  function selectBoard(id: string) {
    if (boards.value.some(b => b.id === id)) activeId.value = id
  }

  const count = computed(() => items.value.length)

  return { boards, board, activeId, items, count, canAddBoard, add, close, duplicate, setPane, setConfig, setSize, setLayout, clear, addBoard, duplicateBoard, removeBoard, updateBoard, selectBoard }
}
