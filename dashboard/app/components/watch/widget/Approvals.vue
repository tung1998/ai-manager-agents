<script setup lang="ts">
// What waits for a yes (actions, diffs proposed by agents), approved or
// skipped right from the box; the card itself one click away.
const { t, when } = useLang()
const toast = useToast()
const { projectId, narrow } = useWatchBox()

const { data, error, refresh } = useLiveFetch<{ incidents: Incident[] }>('/api/incidents', { key: 'incidents', lazy: true }) // pushed as it changes
const list = computed(() => (data.value?.incidents ?? []).filter(x => isApproval(x) && (!projectId.value || x.project_id === projectId.value)))

const busy = ref('')
async function decide(x: Incident, what: 'approve' | 'reject') {
  busy.value = x.key + what
  try {
    await decideApproval(x, what)
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto">
    <div v-if="!data && !error" class="p-2"><LoadingRows :n="3" /></div>
    <div v-else-if="error" class="p-3">
      <UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
    </div>
    <WatchEmpty v-else-if="!list.length" icon="i-lucide-stamp" :text="t('watchW.approvals.none')" />
    <ul v-else class="divide-y divide-(--ui-border)">
      <li v-for="x in list" :key="x.key" class="flex gap-2 px-3 py-2" :class="narrow ? 'flex-col' : 'items-center'">
        <NuxtLink :to="x.link" class="min-w-0 flex-1 hover:underline">
          <span class="block truncate text-sm font-medium">{{ x.title }}</span>
          <span class="block truncate text-xs text-(--ui-text-muted)">{{ [t(`incidents.kind.${x.kind}` as 'incidents.kind.approval'), projectId ? '' : x.project_name, x.at && !x.at.startsWith('0001-') ? when(x.at) : ''].filter(Boolean).join(' · ') }}</span>
        </NuxtLink>
        <div class="flex shrink-0 gap-1">
          <UButton size="xs" icon="i-lucide-check" :label="t('watchW.approvals.approve')" :loading="busy === x.key + 'approve'" :disabled="!!busy" @click="decide(x, 'approve')" />
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-skip-forward" :label="narrow ? undefined : t('watchW.approvals.skip')" :aria-label="t('watchW.approvals.skip')" :loading="busy === x.key + 'reject'" :disabled="!!busy" @click="decide(x, 'reject')" />
        </div>
      </li>
    </ul>
  </div>
</template>
