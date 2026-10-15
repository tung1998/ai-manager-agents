<script setup lang="ts">
// What needs a person (the overview's list), of a project or the office:
// errors first; open it, or let it go from here.
const { t, when } = useLang()
const toast = useToast()
const { projectId, config, narrow } = useWatchBox()

const { data, error, refresh } = useLiveFetch<{ incidents: Incident[] }>('/api/incidents', { lazy: true, server: false })
useWatchPoll(() => refresh())
const list = computed(() => (data.value?.incidents ?? [])
  .filter(x => !isApproval(x) && (!projectId.value || x.project_id === projectId.value) && (!config.value.severity || x.severity === config.value.severity))
  .sort((a, b) => (a.severity === 'error' ? 0 : 1) - (b.severity === 'error' ? 0 : 1)))

const busy = ref('')
async function act(x: Incident, what: IncidentAct) {
  busy.value = x.key
  try {
    await actOnIncident(x, what)
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = ''
  }
}
const at = (x: Incident) => x.at && !x.at.startsWith('0001-') ? when(x.at) : ''
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto">
    <div v-if="!data && !error" class="p-2"><LoadingRows :n="4" /></div>
    <div v-else-if="error" class="p-3">
      <UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
    </div>
    <WatchEmpty v-else-if="!list.length" icon="i-lucide-circle-check" :text="t('watchW.incidents.none')" />
    <ul v-else class="divide-y divide-(--ui-border)">
      <li v-for="x in list" :key="x.key" class="flex items-start gap-2 px-3 py-2">
        <UIcon
          :name="x.severity === 'error' ? 'i-lucide-circle-x' : 'i-lucide-triangle-alert'" class="mt-0.5 size-4 shrink-0"
          :class="x.severity === 'error' ? 'text-(--ui-error)' : 'text-(--ui-warning)'"
        />
        <NuxtLink :to="x.link" class="min-w-0 flex-1 hover:underline">
          <span class="block truncate text-sm font-medium">{{ x.title }}</span>
          <span class="block truncate text-xs text-(--ui-text-muted)">{{ [t(`incidents.kind.${x.kind}` as 'incidents.kind.monitor'), projectId ? '' : x.project_name, narrow ? '' : x.detail].filter(Boolean).join(' · ') }}</span>
        </NuxtLink>
        <span v-if="!narrow" class="shrink-0 text-xs text-(--ui-text-dimmed) tabular-nums">{{ at(x) }}</span>
        <UButton
          v-for="a in incidentActs(x).filter(a => a !== 'dismiss' || !narrow)" :key="a" size="xs" color="neutral" variant="ghost"
          :icon="a === 'seen' ? 'i-lucide-eye' : a === 'continue' ? 'i-lucide-play' : 'i-lucide-x'" :title="t(`watchW.incidents.${a}` as 'watchW.incidents.dismiss')"
          :aria-label="t(`watchW.incidents.${a}` as 'watchW.incidents.dismiss')" :loading="busy === x.key" @click="act(x, a)"
        />
      </li>
    </ul>
  </div>
</template>
