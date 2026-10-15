<script setup lang="ts">
// A project's health checks in a box: one monitor big (state, uptime 24h,
// latency trend) or, with none picked, all of them as a status board.
import type { WatchTone } from '../Status.vue'

interface Point { at: string, ok: boolean, latency_ms: number }
interface Monitor { id: string, name: string, type: string, target: string, status: 'up' | 'down' | 'pending' | 'paused', last_checked_at: string | null, last_latency_ms: number, last_message: string, uptime_24h: number | null, avg_latency_ms: number, trend: Point[] }

const { t, when } = useLang()
const { projectId, config, size, narrow, tiny, short } = useWatchBox()

const { data, error, refresh } = useLiveFetch<{ monitors: Monitor[], summary: Record<string, number> }>('/api/monitors', { query: { project: projectId }, lazy: true, server: false })
useWatchPoll(() => refresh())
const monitors = computed(() => data.value?.monitors ?? [])
const one = computed(() => monitors.value.find(m => m.id === config.value.monitorId) ?? null)

const toneOf = (s: Monitor['status']): WatchTone => ({ up: 'ok', down: 'bad', pending: 'busy', paused: 'idle' } as const)[s]
const uptime = (m: Monitor) => m.uptime_24h == null ? '—' : `${m.uptime_24h.toFixed(m.uptime_24h >= 99.95 || m.uptime_24h < 10 ? 0 : 1)}%`
const opsLink = computed(() => ({ path: `/projects/${projectId.value}`, query: { tab: 'ops', section: 'monitors' } }))
</script>

<template>
  <div class="h-full min-h-0">
    <div v-if="!data && !error" class="p-2"><LoadingRows :n="3" /></div>
    <div v-else-if="error" class="p-3">
      <UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
    </div>
    <WatchEmpty v-else-if="!monitors.length" icon="i-lucide-heart-pulse" :text="t('watchW.monitor.none')">
      <UButton size="sm" color="neutral" variant="soft" icon="i-lucide-arrow-up-right" :label="t('watchW.openOps')" :to="opsLink" />
    </WatchEmpty>

    <!-- one monitor -->
    <div v-else-if="one" class="flex h-full min-h-0 flex-col gap-2 p-3" :class="tiny && 'justify-center'">
      <div class="flex items-center gap-2">
        <WatchStatus :tone="toneOf(one.status)" :label="t(`monitor.status.${one.status}`)" :size="tiny ? 'sm' : 'lg'" />
        <span v-if="tiny" class="min-w-0 flex-1 truncate text-sm font-medium">{{ one.name }}</span>
      </div>
      <template v-if="!tiny">
        <p class="truncate text-xs text-(--ui-text-muted)" :title="one.target">{{ one.name }} · {{ one.target }}</p>
        <div class="grid gap-1.5" :class="narrow ? 'grid-cols-2' : 'grid-cols-3'">
          <WatchTile :label="t('watchW.monitor.uptime')" :value="uptime(one)" icon="i-lucide-activity" />
          <WatchTile :label="t('watchW.monitor.latency')" :value="`${one.last_latency_ms} ms`" icon="i-lucide-gauge" />
          <WatchTile v-if="!narrow" :label="t('watchW.monitor.checked')" :value="one.last_checked_at ? when(one.last_checked_at) : '—'" icon="i-lucide-clock" />
        </div>
        <div v-if="config.trend && !short && one.trend.length > 1" class="flex min-h-0 flex-1 flex-col">
          <p class="text-[11px] text-(--ui-text-dimmed)">{{ t('watchW.monitor.latency') }}</p>
          <WatchSparkline :values="one.trend.map(p => p.latency_ms)" :bad="one.trend.map(p => !p.ok)" :label="t('watchW.monitor.latency')" :fill="size.h > 300" class="min-h-0 flex-1" />
        </div>
        <p v-if="one.status === 'down' && one.last_message" class="line-clamp-3 text-xs text-(--ui-error)">{{ one.last_message }}</p>
      </template>
    </div>

    <!-- all of the project's: a board of rows, a bar of the last checks each when wide -->
    <ul v-else class="h-full divide-y divide-(--ui-border) overflow-y-auto">
      <li v-for="m in monitors" :key="m.id" class="flex items-center gap-2 px-3 py-1.5">
        <WatchStatus :tone="toneOf(m.status)" :label="narrow ? '' : t(`monitor.status.${m.status}`)" class="shrink-0" :class="narrow ? 'w-3' : 'w-16'" />
        <span class="min-w-0 flex-1 truncate text-sm" :title="m.target">{{ m.name }}</span>
        <div v-if="!narrow && config.trend" class="flex h-4 shrink-0 items-end gap-px" :aria-label="t('watchW.monitor.lastChecks')">
          <span v-for="(p, i) in m.trend.slice(-20)" :key="i" class="w-1 rounded-sm" :class="p.ok ? 'h-full bg-(--ui-success)/70' : 'h-full bg-(--ui-error)'" />
        </div>
        <span class="w-12 shrink-0 text-end text-xs text-(--ui-text-muted) tabular-nums">{{ uptime(m) }}</span>
      </li>
    </ul>
  </div>
</template>
