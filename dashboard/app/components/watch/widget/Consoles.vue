<script setup lang="ts">
// Several consoles of a project side by side (like tmux): its running
// commands (or all), as many as asked, laid out by the box's width.
interface Proc { id: string, name: string, cwd: string, state: { status: string } }
const { t } = useLang()
const { projectId, config, size } = useWatchBox()
const { data, error, refresh } = useLiveFetch<{ processes: Proc[] }>(() => `/api/projects/${projectId.value}/processes`, { lazy: true, server: false })
const wanted = computed(() => (data.value?.processes ?? [])
  .filter(p => !config.value.runningOnly || ['running', 'restarting', 'crashed'].includes(p.state.status))
  .slice(0, Number(config.value.max) || 4))
// as many as the box holds (a console under ~140px shows nothing): columns by width, rows by height
const fitCols = computed(() => Math.max(1, Math.floor(size.value.w / 340)))
const fitRows = computed(() => Math.max(1, Math.floor(size.value.h / 140)))
const shown = computed(() => wanted.value.slice(0, fitCols.value * fitRows.value))
const cols = computed(() => Math.min(shown.value.length, fitCols.value, shown.value.length > 3 ? 3 : 2))
const hidden = computed(() => wanted.value.length - shown.value.length)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col p-2">
    <div v-if="!data && !error" class="grid h-full grid-cols-2 gap-2"><USkeleton class="h-full" /><USkeleton class="h-full" /></div>
    <UAlert v-else-if="error" color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
    <WatchEmpty v-else-if="!shown.length" icon="i-lucide-panels-top-left" :text="config.runningOnly ? t('watchW.consoles.noneRunning') : t('watchW.process.none')" />
    <p v-if="shown.length && hidden > 0" class="pb-1 text-xs text-(--ui-text-muted)">{{ t('watchW.consoles.more', { n: hidden }) }}</p>
    <div v-if="shown.length" class="grid min-h-0 flex-1 gap-2" :style="{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`, gridAutoRows: 'minmax(0, 1fr)' }">
      <LogTerminal v-for="p in shown" :key="p.id" fill :url="`/api/processes/${p.id}/stream`" :title="p.name" :subtitle="t(`ops.status.${p.state.status}` as 'ops.status.running')" :empty="t('ops.emptyLog')" />
    </div>
  </div>
</template>
