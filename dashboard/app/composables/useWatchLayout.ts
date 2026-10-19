// The watch screen (like Graylog's dashboards, laid out like a phone's home
// screen): several dashboards, each a grid of boxes taking whole cells (1×1,
// 2×2, 4×4…), each box a widget (a chat, a process, a monitor…). Kept with
// the account (/api/me/prefs/watch), so they follow it to any browser.
import type { WidgetKind } from '~/utils/watchWidgets'

// a box: what it shows and where (x, y, w, h in cells of the 12-column grid)
export interface WatchItem { id: string, widget: WidgetKind, projectId: string, conversationId?: string, config?: Record<string, unknown>, title?: string, x: number, y: number, w: number, h: number }
// projectId: the project a new box starts on
export interface WatchBoard { id: string, name: string, items: WatchItem[], projectId: string }

// boards saved before the grid: a tree of splits whose leaves were boxes
interface OldPane { kind: 'pane', widget?: WidgetKind, projectId: string, conversationId?: string, config?: Record<string, unknown>, title?: string }
type OldNode = OldPane | { kind: 'split', children: OldNode[] }

const PREF_URL = '/api/me/prefs/watch'
export const MAX_BOARDS = 5
// kept in the browser before the account: moved to it once, then dropped
const KEY = 'office.watch.boards'
const OLD_KEY = 'office.watch.layout' // one board, before dashboards
const newId = () => Math.random().toString(36).slice(2, 10)
// the Save button: whether the account holds the last change
export type SaveState = 'saved' | 'dirty' | 'saving' | 'error'
let saveNow: () => Promise<void> = async () => {}
const blank = (items: WatchItem[] = []): WatchBoard => ({ id: newId(), name: '', items, projectId: '' })

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
interface Saved { active: string, boards: WatchBoard[] }

function upgrade(b: WatchBoard & { root?: OldNode | null }): WatchBoard {
  if (Array.isArray(b.items)) {
    // The original chat default was 4×6. Keep intentional custom sizes, but
    // migrate untouched defaults so existing boards benefit from the denser 3×5 layout.
    return {
      ...b,
      items: b.items.map(i => i.widget === 'chat' && i.w === 4 && i.h === 6 ? { ...i, w: 3, h: 5 } : i)
    }
  }
  const { root, ...rest } = b
  return { ...rest, items: fromTree(root ?? null) }
}

export function useWatchLayout() {
  const boards = useState<WatchBoard[]>('watch-boards', () => [])
  const activeId = useState('watch-active', () => '')
  const loaded = useState('watch-layout-loaded', () => false)
  const saveState = useState<SaveState>('watch-save-state', () => 'saved')

  if (import.meta.client && !loaded.value) {
    loaded.value = true
    const { user } = useAuth()
    let saved = '' // what the account holds, so loading it is not saved back
    let ready = false
    let timer: ReturnType<typeof setTimeout> | undefined

    // what this browser kept before the account (the first to sign in takes it)
    const fromBrowser = (): Saved | null => {
      try {
        const raw = localStorage.getItem(KEY)
        if (raw) return JSON.parse(raw) as Saved
        const old = localStorage.getItem(OLD_KEY)
        if (old) return { active: '', boards: [blank(fromTree(JSON.parse(old) as OldNode))] }
      } catch { /* broken or private mode */ }
      return null
    }
    const apply = (s: Saved | null) => {
      boards.value = (s?.boards ?? []).map(upgrade)
      activeId.value = s?.active ?? ''
      if (!boards.value.length) boards.value = [blank()]
      if (!boards.value.some(b => b.id === activeId.value)) activeId.value = boards.value[0]!.id
    }
    let pending = '' // changed but not yet sent
    const save = (value: Saved) => {
      const sent = JSON.stringify(value)
      saveState.value = 'saving'
      return $fetch(PREF_URL, { method: 'PUT', body: { value } })
        .then(() => {
          if (pending === sent) pending = ''
          saveState.value = pending ? 'dirty' : 'saved'
          try {
            localStorage.removeItem(KEY)
            localStorage.removeItem(OLD_KEY)
          } catch { /* private mode */ }
        })
        .catch(() => { // tried again on the next change or the Save button
          saved = ''
          pending = sent
          saveState.value = 'error'
        })
    }
    saveNow = async () => {
      if (!ready) return
      clearTimeout(timer)
      const next = JSON.stringify({ active: activeId.value, boards: boards.value })
      saved = next
      pending = next
      await save(JSON.parse(next) as Saved)
    }

    async function load() {
      ready = false
      clearTimeout(timer)
      let s: Saved | null = null
      try {
        s = (await $fetch<{ value: Saved | null }>(PREF_URL)).value
      } catch { /* offline: start empty, nothing saved until it loads */
        apply(null)
        return
      }
      const local = s ? null : fromBrowser()
      const before = s ? JSON.stringify(s) : ''
      apply(s ?? local)
      saved = JSON.stringify({ active: activeId.value, boards: boards.value })
      ready = true
      if (local || (before && before !== saved)) void save(JSON.parse(saved) as Saved)
    }

    // saved for the app's whole life: made in the first caller's scope, it
    // stopped when that page was left and was never made again (changes then
    // lived only in memory, lost on reload)
    effectScope(true).run(() => {
      // each account its own: loaded again when another signs in
      watch(() => user.value?.id, (id) => {
        if (id) void load()
        else ready = false
      }, { immediate: true })
      watch([boards, activeId], ([list, active]) => {
        if (!ready) return
        const next = JSON.stringify({ active, boards: list })
        if (next === saved) return
        saved = next
        pending = next
        saveState.value = 'dirty'
        clearTimeout(timer)
        timer = setTimeout(() => void save(JSON.parse(next) as Saved), 600)
      }, { deep: true })
    })
    // a reload right after a change came before the delayed save: send it as the page goes
    window.addEventListener('pagehide', () => {
      if (!ready || !pending) return
      clearTimeout(timer)
      void fetch(PREF_URL, { method: 'PUT', keepalive: true, credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ value: JSON.parse(pending) }) })
    })
  }
  if (!boards.value.length) boards.value = [blank()]
  if (!activeId.value) activeId.value = boards.value[0]!.id

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

  return { saveState, save: () => saveNow(), boards, board, activeId, items, count, canAddBoard, add, close, duplicate, setPane, setConfig, setSize, setLayout, clear, addBoard, duplicateBoard, removeBoard, updateBoard, selectBoard }
}
