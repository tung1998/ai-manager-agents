<script setup lang="ts">
// A project's automations by their next run: when, how it went last time;
// the ones turned off at the end (or hidden). Small: the next run as a ring
// filling up to it, the time left inside (a badge), or the next ones in a
// row (a strip).
const { t, when } = useLang()
const { projectId, config, narrow, shape, cells } = useWatchBox()
const { data, error, refresh } = useLiveFetch<{ automations: Automation[] }>(() => `/api/projects/${projectId.value}/automations`, { lazy: true, server: false })
const list = computed(() => (data.value?.automations ?? [])
  .filter(a => a.enabled || config.value.disabled)
  .sort((a, b) => Number(b.enabled) - Number(a.enabled) || (a.next_run_at ?? '9').localeCompare(b.next_run_at ?? '9')))
const now = useNow()
// the next runs; how far along the wait each is (since its last run)
const next = computed(() => list.value.filter(a => a.enabled && a.next_run_at).map((a) => {
  const at = Date.parse(a.next_run_at!)
  const from = a.last_run_at ? Date.parse(a.last_run_at) : at - (a.config.every_minutes || 60) * 60000
  return { a, left: at - now.value, share: Math.min(1, Math.max(0, (now.value - from) / Math.max(1, at - from))) }
}))
const R = 42
const C = 2 * Math.PI * R
const toneClass: Record<string, string> = { info: 'text-(--ui-info)', neutral: 'text-(--ui-text-dimmed)', success: 'text-(--ui-success)', error: 'text-(--ui-error)', warning: 'text-(--ui-warning)' }
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto">
    <div v-if="!data && !error" class="p-2"><LoadingRows :n="4" /></div>
    <div v-else-if="error" class="p-3"><UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" /></div>
    <WatchEmpty v-else-if="!list.length" icon="i-lucide-alarm-clock" :text="t('watchW.automations.none')" />
    <!-- a badge: the next run as a ring filling up to it -->
    <div v-else-if="shape === 'badge' || shape === 'column'" class="flex size-full flex-col items-center justify-center gap-1 p-1">
      <NuxtLink
        v-for="n in next.slice(0, 1)" :key="n.a.id" :to="`/projects/${n.a.project_id}/automations/${n.a.id}`"
        :title="`${n.a.name} · ${scheduleText(n.a, t)}`" class="flex flex-col items-center"
      >
        <span class="relative grid size-12 place-items-center">
          <svg viewBox="0 0 100 100" class="absolute inset-0 -rotate-90">
            <circle cx="50" cy="50" :r="R" fill="none" stroke-width="8" class="stroke-(--ui-bg-accented)" />
            <circle cx="50" cy="50" :r="R" fill="none" stroke-width="8" stroke-linecap="round" class="stroke-(--ui-primary) transition-all duration-1000" :stroke-dasharray="`${n.share * C} ${C}`" />
          </svg>
          <span class="text-xs font-semibold tabular-nums">{{ fmtLeft(n.left, true) }}</span>
        </span>
        <span class="w-full max-w-20 truncate text-center text-[10px] text-(--ui-text-muted)">{{ n.a.name }}</span>
      </NuxtLink>
      <UIcon v-if="!next.length" name="i-lucide-alarm-clock-off" class="size-6 text-(--ui-text-dimmed)" :title="t('watchW.automations.noNext')" />
    </div>
    <!-- a strip: the next runs side by side, a bar filling up to each -->
    <div v-else-if="shape === 'strip'" class="flex size-full items-center gap-3 overflow-hidden px-2.5">
      <NuxtLink
        v-for="n in next.slice(0, Math.max(1, Math.floor(cells.w / 2)))" :key="n.a.id" :to="`/projects/${n.a.project_id}/automations/${n.a.id}`"
        :title="scheduleText(n.a, t)" class="min-w-0 flex-1"
      >
        <span class="flex items-baseline justify-between gap-2 text-xs">
          <span class="truncate">{{ n.a.name }}</span>
          <span class="shrink-0 font-semibold tabular-nums">{{ fmtLeft(n.left) }}</span>
        </span>
        <span class="mt-1 block h-1 overflow-hidden rounded-full bg-(--ui-bg-accented)"><span class="block h-full rounded-full bg-(--ui-primary) transition-all duration-1000" :style="{ width: `${n.share * 100}%` }" /></span>
      </NuxtLink>
      <span v-if="!next.length" class="text-xs text-(--ui-text-muted)">{{ t('watchW.automations.noNext') }}</span>
    </div>
    <ul v-else class="divide-y divide-(--ui-border)">
      <li v-for="a in list" :key="a.id">
        <NuxtLink :to="`/projects/${a.project_id}/automations/${a.id}`" class="flex items-center gap-2 px-3 py-1.5 transition hover:bg-(--ui-bg-elevated)/60" :class="!a.enabled && 'opacity-60'">
          <UIcon
            v-if="a.last_job" :name="jobStatusMeta[a.last_job.status].icon" class="size-4 shrink-0" :title="t(`job.status.${a.last_job.status}`)"
            :class="[toneClass[jobStatusMeta[a.last_job.status].color], { 'animate-spin': a.last_job.status === 'running' }]"
          />
          <UIcon v-else name="i-lucide-circle-dashed" class="size-4 shrink-0 text-(--ui-text-dimmed)" />
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm">{{ a.name }}</span>
            <span v-if="!narrow" class="block truncate text-xs text-(--ui-text-muted)">{{ scheduleText(a, t) }}</span>
          </span>
          <span class="shrink-0 text-xs text-(--ui-text-dimmed) tabular-nums">{{ !a.enabled ? t('watchW.automations.off') : a.next_run_at ? when(a.next_run_at) : '' }}</span>
        </NuxtLink>
      </li>
    </ul>
  </div>
</template>
