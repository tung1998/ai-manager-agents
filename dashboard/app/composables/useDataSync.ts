// Live data (ADR-072): the server says which tables changed (whoever changed
// them: a bot's message, an agent, a run in the background, this page), and
// only what shows those tables refetches. A fetch knows its tables from its
// API path (tablesFor); a list kept by hand says them (useLive).

type Listener = { tables: () => string[], fn: () => void }
const listeners = new Set<Listener>()
const rawListeners = new Set<(tables: string[]) => void>()

// tablesFor: the tables an API path shows ('*' = refresh on anything).
const routes: [RegExp, string[]][] = [
  [/\/memories/, ['agent_memories']],
  [/\/conversations|\/chat\//, ['conversations', 'messages', 'conversation_agents', 'patches', 'actions', 'agents']],
  [/\/automations|\/bots?\b/, ['automations', 'jobs', 'channels']],
  [/\/jobs/, ['jobs', 'automations']],
  [/\/channels|\/limit-alert/, ['channels']],
  [/\/incidents/, ['jobs', 'actions', 'patches', 'channels', 'automations', 'monitors', 'processes', 'providers']],
  [/\/providers|\/usage|\/budget/, ['providers', 'runs', 'jobs']],
  [/\/agents|\/org-models|\/templates/, ['agents', 'org_models']],
  [/\/processes|\/compose|\/monitors|\/monitor-events/, ['processes', 'monitors']],
  [/\/users|\/me\//, ['users']],
  [/\/policy/, ['repos']],
  [/^\/api\/projects(\/[^/?]+)?(\?|$)/, ['repos', 'org_models', 'agents']]
]
export function tablesFor(path: string): string[] {
  const p = path.split('#')[0] ?? ''
  for (const [re, t] of routes) if (re.test(p)) return t
  return []
}

// useLive refreshes fn when one of tables changes (or on '*': back online).
export function useLive(tables: string[] | (() => string[]), fn: () => unknown) {
  const l: Listener = { tables: typeof tables === 'function' ? tables : () => tables, fn: () => { void fn() } }
  listeners.add(l)
  if (getCurrentInstance()) onBeforeUnmount(() => listeners.delete(l))
}

// onLiveChange hears every notice as it comes (the chat's own merging).
export function onLiveChange(fn: (tables: string[]) => void) {
  rawListeners.add(fn)
  if (getCurrentInstance()) onBeforeUnmount(() => rawListeners.delete(fn))
}

// useLiveFetch is useFetch that refreshes when what it shows changes.
export const useLiveFetch = ((url: MaybeRefOrGetter<string>, opts?: object) => {
  const res = useFetch(url as never, opts as never)
  useLive(() => tablesFor(toValue(url) ?? ''), () => res.refresh())
  return res
}) as typeof useFetch

// liveChanged tells the listeners of tables (debounced: one refresh for a burst).
const pending = new Set<string>()
let timer: ReturnType<typeof setTimeout> | undefined
export function liveChanged(tables: string[]) {
  tables.forEach(t => pending.add(t))
  clearTimeout(timer)
  timer = setTimeout(() => {
    const changed = [...pending]
    pending.clear()
    rawListeners.forEach((fn) => { try { fn(changed) } catch { /* one page's error is its own */ } })
    const all = changed.includes('*')
    listeners.forEach((l) => {
      try { if (all || l.tables().some(t => changed.includes(t))) l.fn() } catch { /* its own */ }
    })
  }, 150)
}

// startLive opens the server's change stream once (signed in); it reconnects
// on its own, and a reconnect (office restarted, laptop woke) refreshes all.
let source: EventSource | null = null
export function startLive() {
  if (source || typeof EventSource === 'undefined') return
  source = new EventSource('/api/events')
  let opened = false
  source.addEventListener('open', () => {
    if (opened) liveChanged(['*']) // missed what changed while away
    opened = true
  })
  source.addEventListener('change', (e) => {
    let tables: string[] = ['*']
    try { tables = JSON.parse((e as MessageEvent).data).tables ?? ['*'] } catch { /* a bad notice: refresh all */ }
    liveChanged(tables)
  })
}
