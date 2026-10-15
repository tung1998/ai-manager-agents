<script setup lang="ts">
// The latest jobs (of a project or the office), newest first: state, what,
// who, when; a click opens the job. Columns drop as the box narrows.
const { t, when } = useLang()
const { projectId, config, narrow } = useWatchBox()

const query = computed(() => ({
  limit: Number(config.value.limit) || 20,
  ...(projectId.value ? { project: projectId.value } : {}),
  ...(config.value.status ? { status: config.value.status } : {})
}))
const { data, error, refresh } = useLiveFetch<{ jobs: Job[] }>('/api/jobs', { query, lazy: true, server: false })
useWatchPoll(() => refresh())
const jobs = computed(() => data.value?.jobs ?? [])
const detail = ref<string | null>(null)
const toneClass: Record<string, string> = { info: 'text-(--ui-info)', neutral: 'text-(--ui-text-dimmed)', success: 'text-(--ui-success)', error: 'text-(--ui-error)', warning: 'text-(--ui-warning)' }
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto">
    <div v-if="!data && !error" class="p-2"><LoadingRows :n="5" /></div>
    <div v-else-if="error" class="p-3">
      <UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
    </div>
    <WatchEmpty v-else-if="!jobs.length" icon="i-lucide-list-checks" :text="t('watchW.jobs.none')" />
    <ul v-else class="divide-y divide-(--ui-border)">
      <li v-for="j in jobs" :key="j.id">
        <button type="button" class="flex w-full items-center gap-2 px-3 py-1.5 text-start transition hover:bg-(--ui-bg-elevated)/60" @click="detail = j.id">
          <UIcon
            :name="jobStatusMeta[j.status].icon" class="size-4 shrink-0" :title="t(`job.status.${j.status}`)"
            :class="[toneClass[jobStatusMeta[j.status].color], { 'animate-spin': j.status === 'running' }]"
          />
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm">{{ j.title || j.automation_name || j.agent_name }}</span>
            <span v-if="!narrow" class="block truncate text-xs text-(--ui-text-muted)">{{ [projectId ? '' : j.project_name, j.agent_name, j.error].filter(Boolean).join(' · ') }}</span>
          </span>
          <span v-if="!narrow && j.cost_usd" class="shrink-0 text-xs text-(--ui-text-muted) tabular-nums">${{ j.cost_usd.toFixed(2) }}</span>
          <span class="shrink-0 text-xs text-(--ui-text-dimmed) tabular-nums">{{ when(j.created_at) }}</span>
        </button>
      </li>
    </ul>
    <JobDetailModal :job-id="detail" @close="detail = null" @open="(j: Job) => detail = j.id" />
  </div>
</template>
