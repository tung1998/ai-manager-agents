<script setup lang="ts">
// A project at a glance: a light per process and per monitor (green up, red
// down…), names under them when there is room; hover for the state.
import type { WatchTone } from '../Status.vue'

interface Proc { id: string, name: string, state: { status: string } }
interface Mon { id: string, name: string, status: string }
const { t } = useLang()
const { projectId, config, size } = useWatchBox()
const { data: pd, error: pe, refresh: rp } = useLiveFetch<{ processes: Proc[] }>(() => `/api/projects/${projectId.value}/processes`, { lazy: true, server: false })
const { data: md, refresh: rm } = useLiveFetch<{ monitors: Mon[] }>('/api/monitors', { query: { project: projectId }, lazy: true, server: false })
useWatchPoll(() => { rp(); rm() })

const procTone: Record<string, WatchTone> = { running: 'ok', crashed: 'bad', restarting: 'busy', stopping: 'busy' }
const monTone: Record<string, WatchTone> = { up: 'ok', down: 'bad', pending: 'busy' }
const lights = computed(() => [
  ...(pd.value?.processes ?? []).map(p => ({ id: p.id, name: p.name, icon: 'i-lucide-square-terminal', tone: procTone[p.state.status] ?? 'idle', state: t(`ops.status.${p.state.status}` as 'ops.status.running') })),
  ...(md.value?.monitors ?? []).map(m => ({ id: m.id, name: m.name, icon: 'i-lucide-heart-pulse', tone: monTone[m.status] ?? 'idle', state: t(`monitor.status.${m.status}` as 'monitor.status.up') }))
])
const bg: Record<WatchTone, string> = { ok: 'bg-(--ui-success)', bad: 'bg-(--ui-error)', warn: 'bg-(--ui-warning)', busy: 'bg-(--ui-info)', idle: 'bg-(--ui-text-dimmed)/50' }
const bad = computed(() => lights.value.filter(l => l.tone === 'bad').length)
const labels = computed(() => !!config.value.labels && size.value.w >= 200 && size.value.h >= 150)
// a 1×1 box: dots only, the summary on hover
const small = computed(() => size.value.h < 110 || size.value.w < 110)
const summary = computed(() => bad.value ? t('watchW.lights.bad', { n: bad.value, total: lights.value.length }) : t('watchW.lights.ok', { total: lights.value.length }))
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto" :class="small ? 'p-1.5' : 'p-3'">
    <div v-if="!pd && !pe" class="flex gap-2"><USkeleton v-for="i in 4" :key="i" class="size-8 rounded-md" /></div>
    <p v-else-if="pe" class="text-sm text-(--ui-error)">{{ apiError(pe) }}</p>
    <WatchEmpty v-else-if="!lights.length" icon="i-lucide-lightbulb-off" :text="t('watchW.lights.none')" />
    <template v-else>
      <div v-if="small" class="flex h-full flex-wrap content-center items-center justify-center gap-1" :title="summary">
        <span v-for="l in lights" :key="l.id" class="size-3 rounded-full" :class="[bg[l.tone], { 'animate-pulse': l.tone === 'bad' }]" />
      </div>
      <template v-else>
        <p class="mb-2 truncate text-xs" :class="bad ? 'font-medium text-(--ui-error)' : 'text-(--ui-text-muted)'">{{ summary }}</p>
        <div class="grid gap-1.5" :class="labels ? '[grid-template-columns:repeat(auto-fill,minmax(4.5rem,1fr))]' : '[grid-template-columns:repeat(auto-fill,minmax(1.75rem,1fr))]'">
          <UTooltip v-for="l in lights" :key="l.id" :text="`${l.name}: ${l.state}`">
            <div class="flex min-w-0 flex-col items-center gap-1">
              <span class="flex aspect-square w-full max-w-10 items-center justify-center rounded-md text-white" :class="[bg[l.tone], { 'animate-pulse': l.tone === 'bad' || l.tone === 'busy' }]">
                <UIcon :name="l.icon" class="size-4" />
              </span>
              <span v-if="labels" class="w-full truncate text-center text-[11px] text-(--ui-text-muted)">{{ l.name }}</span>
            </div>
          </UTooltip>
        </div>
      </template>
    </template>
  </div>
</template>
