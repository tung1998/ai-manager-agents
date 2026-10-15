<script setup lang="ts">
// A project's git state: branch, ahead/behind its remote, changed files
// (listed when the box is tall enough).
interface GitStatus { branch: string, upstream?: string, ahead: number, behind: number, changes: { path: string, status: string }[] }
const { t } = useLang()
const { projectId, config, short, tiny } = useWatchBox()
const { data, error, refresh } = useLiveFetch<{ repo: boolean, error?: string, status?: GitStatus }>(() => `/api/projects/${projectId.value}/git`, { lazy: true, server: false })
useWatchPoll(() => refresh())
const st = computed(() => data.value?.status)
const statusClass: Record<string, string> = { 'M': 'text-(--ui-warning)', 'A': 'text-(--ui-success)', 'D': 'text-(--ui-error)', '??': 'text-(--ui-text-dimmed)' }
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-2 p-3">
    <div v-if="!data && !error" class="space-y-2"><USkeleton class="h-6 w-2/3" /><USkeleton class="h-4 w-1/2" /></div>
    <p v-else-if="error" class="text-sm text-(--ui-error)">{{ apiError(error) }}</p>
    <WatchEmpty v-else-if="!data!.repo || !st" icon="i-lucide-git-branch" :text="data!.error || t('watchW.git.noRepo')" />
    <template v-else>
      <div class="flex min-w-0 items-center gap-2">
        <UIcon name="i-lucide-git-branch" class="size-5 shrink-0 text-(--ui-primary)" />
        <span class="truncate text-lg font-semibold">{{ st.branch }}</span>
      </div>
      <div class="flex flex-wrap gap-x-3 gap-y-1 text-xs tabular-nums">
        <span :class="st.changes.length ? 'text-(--ui-warning)' : 'text-(--ui-success)'">
          {{ st.changes.length ? t('watchW.git.changed', { n: st.changes.length }) : t('watchW.git.clean') }}
        </span>
        <span v-if="st.upstream" class="text-(--ui-text-muted)" :title="st.upstream">↑{{ st.ahead }} ↓{{ st.behind }}</span>
      </div>
      <ul v-if="config.files && !short && !tiny && st.changes.length" class="min-h-0 flex-1 space-y-0.5 overflow-y-auto font-mono text-xs">
        <li v-for="c in st.changes" :key="c.path" class="flex gap-2">
          <span class="w-5 shrink-0" :class="statusClass[c.status] ?? 'text-(--ui-text-muted)'">{{ c.status }}</span>
          <span class="truncate" :title="c.path">{{ c.path }}</span>
        </li>
      </ul>
    </template>
  </div>
</template>
