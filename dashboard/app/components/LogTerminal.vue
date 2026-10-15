<script setup lang="ts">
// Terminal-like log view of a console: a process's output, the compose
// action's, a service's container logs. Followed on the tab's one event
// stream (ADR-158), not an SSE of its own, so a board of consoles does not
// take all the browser's connections to office; an address it does not know
// still gets its own stream.
import type { LogLine, LogSpec } from '~/composables/useDataSync'

type Line = LogLine
const { t, dateLocale } = useLang()

// fill: takes its box's height (a watch box) instead of a fixed one
const props = defineProps<{ url: string | null, title: string, subtitle?: string, empty?: string, fill?: boolean }>()
const lines = ref<Line[]>([])
const el = ref<HTMLElement | null>(null)
const follow = ref(true)
let source: EventSource | null = null
let stopLog: (() => void) | null = null

// the console an address names (the addresses the panels have always used)
function specOf(url: string): LogSpec | null {
  const u = new URL(url, 'http://x')
  let m = u.pathname.match(/^\/api\/processes\/([^/]+)\/stream$/)
  if (m) return { source: 'process', id: decodeURIComponent(m[1]!) }
  m = u.pathname.match(/^\/api\/projects\/([^/]+)\/compose\/action\/stream$/)
  if (m) return { source: 'action', id: decodeURIComponent(m[1]!) }
  m = u.pathname.match(/^\/api\/projects\/([^/]+)\/compose\/logs$/)
  if (m) return { source: 'compose', id: decodeURIComponent(m[1]!), file: u.searchParams.get('file') ?? '', service: u.searchParams.get('service') ?? '' }
  return null
}

function add(more: Line[]) {
  lines.value = [...lines.value, ...more].slice(-3000)
  if (follow.value) nextTick(() => el.value?.scrollTo({ top: el.value.scrollHeight }))
}
function close() {
  source?.close()
  source = null
  stopLog?.()
  stopLog = null
}
watch(() => props.url, (url) => {
  close()
  lines.value = []
  follow.value = true
  if (!url || !import.meta.client) return
  const own = () => {
    source = new EventSource(url)
    source.addEventListener('lines', ev => add(JSON.parse((ev as MessageEvent).data) as Line[]))
  }
  const spec = specOf(url)
  if (spec) stopLog = followLog(spec, add, () => { lines.value = [] }, () => { stopLog = null; own() })
  else own()
}, { immediate: true })
onBeforeUnmount(close)

function onScroll() {
  const e = el.value
  if (e) follow.value = e.scrollHeight - e.scrollTop - e.clientHeight < 40
}
function toBottom() {
  follow.value = true
  el.value?.scrollTo({ top: el.value.scrollHeight })
}
const time = (iso: string) => new Date(iso).toLocaleTimeString(dateLocale.value, { hour12: false })
</script>

<template>
  <div class="flex flex-col overflow-hidden rounded-lg border border-(--ui-border) bg-neutral-950" :class="fill ? 'h-full min-h-0' : 'h-[26rem] max-h-[60vh] self-start'">
    <div class="flex items-center gap-2 border-b border-white/10 px-3 py-2 text-xs text-neutral-400">
      <UIcon name="i-lucide-terminal" class="size-4 shrink-0" />
      <span class="truncate font-medium text-neutral-200">{{ title }}</span>
      <span v-if="subtitle" class="truncate">{{ subtitle }}</span>
      <slot name="actions" />
      <span class="ms-auto shrink-0">{{ t('log.lines', { n: lines.length }) }}</span>
      <button type="button" class="shrink-0 hover:text-neutral-200" @click="lines = []">{{ t('log.clear') }}</button>
    </div>
    <div ref="el" class="min-h-0 flex-1 overflow-auto p-3 font-mono text-xs leading-relaxed" @scroll="onScroll">
      <p v-if="!lines.length" class="text-neutral-500">{{ empty ?? t('log.empty') }}</p>
      <div v-for="l in lines" :key="l.seq" class="flex gap-3 whitespace-pre-wrap break-all">
        <span class="shrink-0 select-none text-neutral-600">{{ time(l.time) }}</span>
        <span :class="l.stream === 'err' ? 'text-red-300' : l.stream === 'sys' ? 'text-sky-300' : 'text-neutral-200'">{{ l.text }}</span>
      </div>
    </div>
    <button v-if="!follow" type="button" class="border-t border-white/10 py-1 text-xs text-neutral-400 hover:text-neutral-200" @click="toBottom">
      {{ t('log.toBottom') }}
    </button>
  </div>
</template>
