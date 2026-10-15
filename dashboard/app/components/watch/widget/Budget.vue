<script setup lang="ts">
// What AI cost today against the day's limit (the office's, or a project's):
// a ring when there is a limit, the amount alone when there is none.
interface Usage { today: number, daily_limit: number, project_today: Record<string, number>, settings: { project_limits?: Record<string, number> } }
const { t } = useLang()
const { projectId, size, tiny } = useWatchBox()
const { data, error, refresh } = useLiveFetch<Usage>('/api/usage/summary', { query: { days: 1 }, lazy: true, server: false })
useWatchPoll(() => refresh())
const spent = computed(() => projectId.value ? data.value?.project_today[projectId.value] ?? 0 : data.value?.today ?? 0)
const limit = computed(() => projectId.value ? data.value?.settings.project_limits?.[projectId.value] ?? 0 : data.value?.daily_limit ?? 0)
const pct = computed(() => limit.value ? spent.value / limit.value * 100 : 0)
const money = (v: number) => `$${v < 100 ? v.toFixed(2) : Math.round(v).toLocaleString()}`
const ring = computed(() => Math.max(64, Math.min(size.value.w - 24, size.value.h - 56, 160)))
// the amount as large as the box allows (a 1×1 box: just the figure)
const fontPx = computed(() => Math.round(Math.max(16, Math.min(size.value.w / (money(spent.value).length * 0.62), size.value.h / 2.4, 40))))
const small = computed(() => size.value.h < 110)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col items-center justify-center gap-1 overflow-hidden" :class="small ? 'p-1.5' : 'p-3'">
    <USkeleton v-if="!data && !error" class="size-20 rounded-full" />
    <p v-else-if="error" class="text-sm text-(--ui-error)">{{ apiError(error) }}</p>
    <template v-else-if="limit && !tiny">
      <WatchRing :value="pct" :size="ring" :label="t('watchW.budget.title')">
        <span class="block text-lg font-semibold">{{ money(spent) }}</span>
        <span class="block text-[11px] text-(--ui-text-muted)">/ {{ money(limit) }}</span>
      </WatchRing>
      <p class="text-xs text-(--ui-text-muted)">{{ t('watchW.budget.today', { n: Math.round(pct) }) }}</p>
    </template>
    <template v-else>
      <p class="font-semibold leading-none" :class="limit && pct >= 90 ? 'text-(--ui-error)' : ''" :style="{ fontSize: `${fontPx}px` }" :title="t('watchW.budget.title')">{{ money(spent) }}</p>
      <p v-if="!small" class="line-clamp-2 text-center text-xs text-(--ui-text-muted)">{{ limit ? t('watchW.budget.of', { limit: money(limit) }) : t('watchW.budget.noLimit') }}</p>
      <p v-else class="truncate text-[11px] text-(--ui-text-muted)">{{ limit ? `${Math.round(pct)}%` : t('watchW.budget.short') }}</p>
    </template>
  </div>
</template>
