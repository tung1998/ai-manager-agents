<script setup lang="ts">
// A project's switchboard (admins): every command, and its compose services
// if asked, a row each with its light and start/stop/restart.
import type { WatchTone } from '../Status.vue'

interface Proc { id: string, name: string, state: { status: string } }
interface Svc { name: string, container?: { state: string } }
const { t } = useLang()
const toast = useToast()
const { projectId, config, narrow } = useWatchBox()
const { data, error, refresh } = useLiveFetch<{ processes: Proc[] }>(() => `/api/projects/${projectId.value}/processes`, { lazy: true, server: false })
const { data: cd, refresh: rc } = useLiveFetch<{ file: string, docker: { available: boolean }, services: Svc[] }>(() => `/api/projects/${projectId.value}/compose`, { lazy: true, server: false })

const procTone: Record<string, WatchTone> = { running: 'ok', crashed: 'bad', restarting: 'busy', stopping: 'busy' }
const rows = computed(() => [
  ...(data.value?.processes ?? []).map(p => ({ key: `p:${p.id}`, kind: 'process' as const, id: p.id, name: p.name, on: ['running', 'restarting', 'stopping'].includes(p.state.status), tone: procTone[p.state.status] ?? 'idle', state: t(`ops.status.${p.state.status}` as 'ops.status.running') })),
  ...(config.value.services && cd.value?.docker.available ? cd.value.services : []).map(s => ({ key: `s:${s.name}`, kind: 'service' as const, id: s.name, name: s.name, on: s.container?.state === 'running', tone: (s.container?.state === 'running' ? 'ok' : 'idle') as WatchTone, state: s.container?.state === 'running' ? t('compose.state.running') : t('compose.state.stopped') }))
])

const busy = ref('')
async function act(r: (typeof rows.value)[number], action: 'start' | 'stop' | 'restart') {
  busy.value = r.key + action
  try {
    if (r.kind === 'process') await $fetch(`/api/processes/${r.id}/${action}`, { method: 'POST' })
    else await $fetch(`/api/projects/${projectId.value}/compose/action`, { method: 'POST', body: { file: cd.value?.file ?? '', action: action === 'start' ? 'up' : action, service: r.id } })
    await (r.kind === 'process' ? refresh() : rc())
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto">
    <div v-if="!data && !error" class="p-2"><LoadingRows :n="4" /></div>
    <div v-else-if="error" class="p-3"><UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" /></div>
    <WatchEmpty v-else-if="!rows.length" icon="i-lucide-sliders-horizontal" :text="t('watchW.process.none')" />
    <ul v-else class="divide-y divide-(--ui-border)">
      <li v-for="r in rows" :key="r.key" class="flex items-center gap-2 px-3 py-1.5">
        <UIcon :name="r.kind === 'process' ? 'i-lucide-square-terminal' : 'i-lucide-container'" class="size-4 shrink-0 text-(--ui-text-dimmed)" />
        <span class="min-w-0 flex-1 truncate text-sm">{{ r.name }}</span>
        <WatchStatus :tone="r.tone" :label="narrow ? '' : r.state" class="shrink-0" />
        <UButton v-if="!r.on" size="xs" icon="i-lucide-play" :aria-label="t('ops.start')" :title="t('ops.start')" :loading="busy === r.key + 'start'" @click="act(r, 'start')" />
        <template v-else>
          <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-square" :aria-label="t('ops.stop')" :title="t('ops.stop')" :loading="busy === r.key + 'stop'" @click="act(r, 'stop')" />
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-rotate-cw" :aria-label="t('ops.restart')" :title="t('ops.restart')" :loading="busy === r.key + 'restart'" @click="act(r, 'restart')" />
        </template>
      </li>
    </ul>
  </div>
</template>
