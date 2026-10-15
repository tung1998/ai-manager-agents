<script setup lang="ts">
// A web page in the box (a dev server while you work on it, a status page):
// from a command's port or any address; reloads on its own if asked. Some
// sites refuse to be shown in a frame: open them in a tab instead.
interface Proc { id: string, name: string, state: { status: string, port?: number } }
const { t } = useLang()
const { projectId, config } = useWatchBox()
const { data } = useLiveFetch<{ processes: Proc[] }>(() => `/api/projects/${projectId.value || '_'}/processes`, { lazy: true, server: false, immediate: !!projectId.value })
const proc = computed(() => data.value?.processes.find(p => p.id === config.value.processId))
// the command's port on the machine this page came from (not this browser's localhost)
const url = computed(() => {
  const u = String(config.value.url ?? '').trim()
  if (u) return /^https?:\/\//.test(u) ? u : `http://${u}`
  const port = proc.value?.state.port
  return port && import.meta.client ? `${location.protocol}//${location.hostname}:${port}` : ''
})
const n = ref(0) // bumped to reload the frame
let timer: ReturnType<typeof setInterval> | undefined
watch(() => Number(config.value.reload), (s) => {
  clearInterval(timer)
  if (s > 0) timer = setInterval(() => n.value++, s * 1000)
}, { immediate: true })
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <WatchEmpty v-if="!url" icon="i-lucide-app-window" :text="proc && !proc.state.port ? t('watchW.preview.noPort') : t('watchW.preview.pick')" />
    <template v-else>
      <div class="flex shrink-0 items-center gap-1 border-b border-(--ui-border) px-2 py-1">
        <UIcon name="i-lucide-globe" class="size-3.5 shrink-0 text-(--ui-text-dimmed)" />
        <span class="min-w-0 flex-1 truncate text-xs text-(--ui-text-muted)">{{ url }}</span>
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-rotate-cw" :aria-label="t('watchW.preview.reload')" :title="t('watchW.preview.reload')" @click="n++" />
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-external-link" :aria-label="t('watchW.preview.open')" :title="t('watchW.preview.open')" :to="url" target="_blank" />
      </div>
      <iframe :key="`${url}#${n}`" :src="url" :title="url" class="min-h-0 w-full flex-1 bg-white" sandbox="allow-scripts allow-same-origin allow-forms allow-popups" />
    </template>
  </div>
</template>
