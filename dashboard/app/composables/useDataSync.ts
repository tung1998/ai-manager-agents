// Lists stay in step with what the viewer just changed: after a successful
// add/edit/delete (and when the tab comes back into view) every useFetch on
// screen refetches, and lists kept by hand (the sidebar's projects, the job
// table) listen with onDataChanged.
const listeners = new Set<() => void>()
const live = new Set<(tables: string[]) => void>()

// onLiveChange hears what the server says changed (the tables), whoever
// changed it: a bot's message, an agent, a run in the background (ADR-072).
export function onLiveChange(fn: (tables: string[]) => void) {
  live.add(fn)
  if (getCurrentInstance()) onBeforeUnmount(() => live.delete(fn))
}

// startLive opens the server's change stream once (signed in); it reconnects
// on its own when the office restarts.
let source: EventSource | null = null
export function startLive() {
  if (source || typeof EventSource === 'undefined') return
  source = new EventSource('/api/events')
  source.addEventListener('change', (e) => {
    let tables: string[] = []
    try { tables = JSON.parse((e as MessageEvent).data).tables ?? [] } catch { /* a bad notice: refresh all */ }
    live.forEach((fn) => {
      try { fn(tables) } catch { /* one page's error is its own */ }
    })
    dataChanged()
  })
}
export function stopLive() {
  source?.close()
  source = null
}

export function onDataChanged(fn: () => void) {
  listeners.add(fn)
  if (getCurrentInstance()) onBeforeUnmount(() => listeners.delete(fn))
}

let timer: ReturnType<typeof setTimeout> | undefined
export function dataChanged() {
  clearTimeout(timer)
  timer = setTimeout(() => { // one refetch for a burst of changes
    refreshNuxtData()
    listeners.forEach((fn) => {
      try { fn() } catch { /* one list's error is its own */ }
    })
  }, 200)
}
