<script setup lang="ts">
// The work of the last days as columns (done, failed) or what it cost per
// day: the overview's chart for one project or the whole office.
import type { WatchBar } from '../Bars.vue'

interface Row { key: string, jobs: number, done: number, failed: number, cost_usd: number }
const { t, dateLocale } = useLang()
const { projectId, config, size } = useWatchBox()

const days = computed(() => Number(config.value.days) || 14)
const query = computed(() => ({ days: days.value, ...(projectId.value ? { project: projectId.value } : {}) }))
const { data, error, refresh } = useLiveFetch<{ rows: Row[] }>('/api/jobs/stats', { query, lazy: true, server: false })
useWatchPoll(() => refresh())

const cost = computed(() => config.value.measure === 'cost')
// every day of the range, one without work too (the columns keep their place)
const bars = computed<WatchBar[]>(() => {
  const by = new Map((data.value?.rows ?? []).map(r => [r.key, r]))
  const out: WatchBar[] = []
  for (let i = days.value - 1; i >= 0; i--) {
    const d = new Date(Date.now() - i * 86400000)
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    const r = by.get(key)
    const label = d.toLocaleDateString(dateLocale.value, { day: 'numeric', month: 'numeric' })
    if (cost.value) out.push({ key, label, parts: [r?.cost_usd ?? 0], tip: `${label}: $${(r?.cost_usd ?? 0).toFixed(2)}` })
    else {
      const done = r?.done ?? 0
      const failed = r?.failed ?? 0
      const other = Math.max(0, (r?.jobs ?? 0) - done - failed)
      out.push({ key, label, parts: [done + other, failed], tip: t('watchW.chart.tip', { day: label, n: r?.jobs ?? 0, failed }) })
    }
  }
  return out
})
const total = computed(() => cost.value
  ? `$${(data.value?.rows ?? []).reduce((a, r) => a + r.cost_usd, 0).toFixed(2)}`
  : (data.value?.rows ?? []).reduce((a, r) => a + r.jobs, 0).toLocaleString())
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-2 p-3">
    <div class="flex items-baseline justify-between gap-2">
      <p class="truncate text-xs font-medium text-(--ui-text-muted)">{{ cost ? t('watchW.chart.cost', { n: days }) : t('watchW.chart.jobs', { n: days }) }}</p>
      <p class="shrink-0 text-sm font-semibold tabular-nums">{{ data ? total : '' }}</p>
    </div>
    <USkeleton v-if="!data && !error" class="min-h-0 flex-1" />
    <UAlert v-else-if="error" color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
    <WatchBars
      v-else-if="size.h >= 90" class="min-h-0 flex-1" :bars="bars"
      :colors="cost ? ['bg-(--ui-primary)'] : ['bg-(--ui-primary)', 'bg-(--ui-error)']"
      :legend="cost || size.h < 160 ? undefined : [t('watchW.chart.done'), t('watchW.chart.failed')]"
    />
  </div>
</template>
