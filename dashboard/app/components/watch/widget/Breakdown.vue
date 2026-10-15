<script setup lang="ts">
// Where the money went: projects or models as bars, the biggest first; the
// rest folded into "others" so the colors never run out.
interface Row { key: string, label: string, runs: number, cost_usd: number }
const { t } = useLang()
const { config, size } = useWatchBox()
const days = computed(() => Number(config.value.days) || 7)
const { data, error, refresh } = useLiveFetch<{ by_project: Row[], by_model: Row[] }>('/api/usage/summary', { query: { days }, lazy: true, server: false })
useWatchPoll(() => refresh())
const rows = computed(() => {
  const all = [...(config.value.by === 'model' ? data.value?.by_model ?? [] : data.value?.by_project ?? [])].sort((a, b) => b.cost_usd - a.cost_usd)
  const n = Math.max(3, Math.floor((size.value.h - 40) / 34))
  if (all.length <= n) return all
  const rest = all.slice(n - 1)
  return [...all.slice(0, n - 1), { key: '_rest', label: t('watchW.breakdown.others', { n: rest.length }), runs: rest.reduce((a, r) => a + r.runs, 0), cost_usd: rest.reduce((a, r) => a + r.cost_usd, 0) }]
})
const max = computed(() => Math.max(0.0001, ...rows.value.map(r => r.cost_usd)))
const total = computed(() => rows.value.reduce((a, r) => a + r.cost_usd, 0))
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-2 p-3">
    <div class="flex items-baseline justify-between gap-2">
      <p class="truncate text-xs font-medium text-(--ui-text-muted)">{{ t(config.by === 'model' ? 'watchW.breakdown.byModel' : 'watchW.breakdown.byProject', { n: days }) }}</p>
      <p v-if="data" class="shrink-0 text-sm font-semibold tabular-nums">${{ total.toFixed(2) }}</p>
    </div>
    <LoadingRows v-if="!data && !error" :n="4" />
    <p v-else-if="error" class="text-sm text-(--ui-error)">{{ apiError(error) }}</p>
    <WatchEmpty v-else-if="!rows.length" icon="i-lucide-chart-bar" :text="t('watchW.breakdown.none')" />
    <ul v-else class="min-h-0 flex-1 space-y-2 overflow-y-auto">
      <li v-for="r in rows" :key="r.key">
        <div class="flex items-baseline justify-between gap-2 text-xs">
          <span class="truncate">{{ r.label || r.key }}</span>
          <span class="shrink-0 text-(--ui-text-muted) tabular-nums">${{ r.cost_usd.toFixed(2) }} · {{ t('watchW.breakdown.runs', { n: r.runs }) }}</span>
        </div>
        <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-(--ui-primary)/10">
          <div class="h-full rounded-full" :class="r.key === '_rest' ? 'bg-(--ui-text-dimmed)/60' : 'bg-(--ui-primary)'" :style="{ width: `${r.cost_usd / max * 100}%` }" />
        </div>
      </li>
    </ul>
  </div>
</template>
