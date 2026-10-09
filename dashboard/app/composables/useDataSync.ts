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
  [/\/burn/, ['burn_sessions', 'burn_items']],
  [/\/audit/, ['audit_log']],
  [/\/stats\b/, ['runs', 'jobs']],
  [/\/assistant/, ['conversations', 'messages', 'settings']],
  [/\/system/, ['*']],
  [/\/conversations|\/chat\//, ['conversations', 'messages', 'conversation_agents', 'patches', 'actions', 'agents']],
  [/\/automations|\/bots?\b/, ['automations', 'jobs', 'channels']],
  [/\/jobs/, ['jobs', 'automations']],
  [/\/limit-alert/, ['settings', 'channels']],
  [/\/channels/, ['channels']],
  [/\/mcp\/servers/, ['mcp_servers']],
  [/\/providers|\/usage|\/budget/, ['providers', 'runs', 'jobs', 'settings']],
  [/\/workflow/, ['workflows', 'workflow_runs', 'agents']],
  [/\/revisions/, ['revisions', 'agents', 'repos']],
  [/\/agents|\/packs/, ['agents', 'repos']],
  [/\/processes|\/compose|\/monitors|\/monitor-events/, ['processes', 'monitors', 'monitor_events']],
  [/\/users|\/me\//, ['users']],
  [/\/policy/, ['settings', 'repos']],
  [/^\/api\/projects(\/[^/?]+)?(\?|$)/, ['repos', 'agents']]
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

// Kept data (ADR-127): what a page loaded stays when it is left; coming
// back shows it at once. It is loaded again only when one of its tables
// changed meanwhile (the stream says so) or the stream was down; then the
// kept data shows while the new loads.
const kept = new Map<string, { tables: string[], stale: boolean }>()
function markStale(changed: string[]) {
  const all = changed.includes('*')
  kept.forEach((k) => { if (all || k.tables.includes('*') || k.tables.some(t => changed.includes(t))) k.stale = true })
}
function keptData(tables: () => string[]) {
  return (key: string, app: ReturnType<typeof useNuxtApp>, ctx: { cause: string }) => {
    const k = kept.get(key)
    kept.set(key, { tables: tables(), stale: false }) // what is fetched now is current
    if (ctx.cause !== 'initial' || !k) return undefined
    const data = app.payload.data[key]
    if (data === undefined) return undefined
    if (k.stale) setTimeout(() => { void app.callHook('app:data:refresh', [key]) }) // show what was kept, load the new behind it
    return data
  }
}
function keepOpts(opts: object | undefined, tables: () => string[]) {
  return { getCachedData: keptData(tables), ...opts }
}

// usePushedFetch is useFetch for data the server pushes as it changes
// (ADR-078): the page puts each change in place; loaded again only back
// online (what came meanwhile may be missed), or on coming back to it after
// its tables changed.
export const usePushedFetch = ((url: MaybeRefOrGetter<string>, opts?: object) => {
  const res = useFetch(url as never, keepOpts(opts, () => tablesFor(toValue(url) ?? '')) as never)
  useLive(['*'], () => res.refresh())
  return res
}) as typeof useFetch

// onLiveEvent hears an event the server pushes with its data (ADR-078):
// message, conversation, conversation.deleted, incidents…
const eventListeners = new Map<string, Set<(data: any) => void>>() // eslint-disable-line @typescript-eslint/no-explicit-any
export function onLiveEvent<T = any>(name: string, fn: (data: T) => void) { // eslint-disable-line @typescript-eslint/no-explicit-any
  if (!eventListeners.has(name)) eventListeners.set(name, new Set())
  eventListeners.get(name)!.add(fn)
  if (getCurrentInstance()) onBeforeUnmount(() => eventListeners.get(name)?.delete(fn))
}
const pushed = ['message', 'conversation', 'conversation.deleted', 'incidents', 'mcp.status', 'git']
let nuxtApp: ReturnType<typeof tryUseNuxtApp> = null

// onLiveChange hears every notice as it comes (the chat's own merging).
export function onLiveChange(fn: (tables: string[]) => void) {
  rawListeners.add(fn)
  if (getCurrentInstance()) onBeforeUnmount(() => rawListeners.delete(fn))
}

// useLiveFetch is useFetch that refreshes when what it shows changes.
export const useLiveFetch = ((url: MaybeRefOrGetter<string>, opts?: object) => {
  const res = useFetch(url as never, keepOpts(opts, () => tablesFor(toValue(url) ?? '')) as never)
  const lazyStart = (opts as { immediate?: boolean } | undefined)?.immediate === false
  useLive(() => tablesFor(toValue(url) ?? ''), () => {
    if (lazyStart && res.status.value === 'idle') return // not started on purpose: a change does not start it
    return res.refresh()
  })
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
    markStale(changed) // kept data of pages not open; an open page refreshes now
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
let sid: number | null = null // this stream's id, for the topics it turns on

// liveTopic turns a topic of this tab's stream on or off (once it is open;
// a new stream turns them on again).
export async function liveTopic(topic: string, on: boolean) {
  if (sid === null) return
  try {
    await $fetch('/api/events/topics', { method: 'POST', body: { sid, topic, on } })
  } catch { /* the stream may be reconnecting: it asks again */ }
}
// followTurn follows an answer being written on this tab's one stream
// (ADR-127), not a stream of its own: a browser opens only a few
// connections to office, and every request waits behind them. fn gets its
// events in order (through reconnects); gone: it had ended before it could be
// followed (reload what it wrote). The returned func stops following.
export interface TurnEvent { seq: number, type: string }
interface Followed { next: number, fn: (ev: any) => void, gone: () => void } // eslint-disable-line @typescript-eslint/no-explicit-any
const turns = new Map<string, Followed>()
export function followTurn<T extends TurnEvent>(id: string, fn: (ev: T) => void, gone: () => void) {
  const f: Followed = { next: 0, fn, gone }
  turns.set(id, f)
  void askTurn(id, f)
  return () => {
    if (turns.get(id) !== f) return
    turns.delete(id)
    if (sid !== null) $fetch('/api/events/turns', { method: 'POST', body: { sid, turn: id, on: false } }).catch(() => {})
  }
}
async function askTurn(id: string, f: Followed) {
  const at = sid
  if (at === null) return // the stream's hello asks
  try {
    await $fetch('/api/events/turns', { method: 'POST', body: { sid: at, turn: id, from: f.next, on: true } })
  } catch {
    if (turns.get(id) !== f || sid !== at) return // stopped, or a new stream asks again
    turns.delete(id)
    f.gone()
  }
}
function onTurn(d: { turn: string, events: TurnEvent[] }) {
  const f = turns.get(d.turn)
  for (const ev of d.events ?? []) {
    if (!f || turns.get(d.turn) !== f) return // stopped meanwhile
    if (ev.seq < f.next) continue // seen before a reconnect
    f.next = ev.seq + 1
    if (ev.type === 'done' || ev.type === 'error') turns.delete(d.turn)
    try { f.fn(ev) } catch { /* its own */ }
  }
}

let retry: ReturnType<typeof setTimeout> | undefined
let backoff = 1000
export function startLive() {
  if (source || typeof EventSource === 'undefined') return
  const es = new EventSource('/api/events')
  source = es
  let opened = false
  es.addEventListener('open', () => {
    if (opened || backoff > 1000) liveChanged(['*']) // missed what changed while away
    opened = true
    backoff = 1000
  })
  // a refused stream (signed out, office restarting: 401/502) is not retried
  // by the browser: try again, slower each time
  es.addEventListener('error', () => {
    if (source === es) sid = null // its id ends with it: the next stream's hello says the new one
    if (es.readyState !== EventSource.CLOSED || source !== es) return
    source = null
    clearTimeout(retry)
    retry = setTimeout(startLive, backoff)
    backoff = Math.min(backoff * 2, 30000)
  })
  es.addEventListener('hello', (e) => {
    try { sid = JSON.parse((e as MessageEvent).data).sid } catch { sid = null }
    turns.forEach((f, id) => { void askTurn(id, f) }) // a new stream: follow them again from where they were
    onSysStream()
  })
  // "Cần xử lý", worked out by the server for this person: put in place
  // (once: a reconnect runs this again, outside any component)
  if (!nuxtApp) {
    nuxtApp = tryUseNuxtApp()
    if (!eventListeners.has('incidents')) eventListeners.set('incidents', new Set())
    eventListeners.get('incidents')!.add((d) => { void nuxtApp?.runWithContext(() => { useNuxtData('incidents').data.value = d }) })
  }
  for (const name of pushed) {
    es.addEventListener(name, (e) => {
      let data: unknown
      try { data = JSON.parse((e as MessageEvent).data) } catch { return }
      eventListeners.get(name)?.forEach((fn) => { try { fn(data) } catch { /* its own */ } })
    })
  }
  es.addEventListener('turn', (e) => {
    try { onTurn(JSON.parse((e as MessageEvent).data)) } catch { /* a bad one */ }
  })
  for (const name of ['stats', 'machine']) {
    es.addEventListener(name, (e) => {
      try { onSysEvent(name, JSON.parse((e as MessageEvent).data)) } catch { /* a bad one: the next comes */ }
    })
  }
  es.addEventListener('change', (e) => {
    let tables: string[] = ['*']
    try { tables = JSON.parse((e as MessageEvent).data).tables ?? ['*'] } catch { /* a bad notice: refresh all */ }
    liveChanged(tables)
  })
}

// stopLive closes the stream (signing out).
export function stopLive() {
  sid = null
  clearTimeout(retry)
  source?.close()
  source = null
}
