// Lists stay in step with what the viewer just changed: after a successful
// add/edit/delete (and when the tab comes back into view) every useFetch on
// screen refetches, and lists kept by hand (the sidebar's projects, the job
// table) listen with onDataChanged.
const listeners = new Set<() => void>()

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
