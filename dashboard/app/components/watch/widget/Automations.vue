<script setup lang="ts">
// A project's automations by their next run: when, how it went last time;
// the ones turned off at the end (or hidden).
const { t, when } = useLang()
const { projectId, config, narrow } = useWatchBox()
const { data, error, refresh } = useLiveFetch<{ automations: Automation[] }>(() => `/api/projects/${projectId.value}/automations`, { lazy: true, server: false })
const list = computed(() => (data.value?.automations ?? [])
  .filter(a => a.enabled || config.value.disabled)
  .sort((a, b) => Number(b.enabled) - Number(a.enabled) || (a.next_run_at ?? '9').localeCompare(b.next_run_at ?? '9')))
const toneClass: Record<string, string> = { info: 'text-(--ui-info)', neutral: 'text-(--ui-text-dimmed)', success: 'text-(--ui-success)', error: 'text-(--ui-error)', warning: 'text-(--ui-warning)' }
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto">
    <div v-if="!data && !error" class="p-2"><LoadingRows :n="4" /></div>
    <div v-else-if="error" class="p-3"><UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" /></div>
    <WatchEmpty v-else-if="!list.length" icon="i-lucide-alarm-clock" :text="t('watchW.automations.none')" />
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
