<script setup lang="ts">
// What waits for a yes (actions, diffs proposed by agents), approved or
// skipped right from the box; the card itself one click away. Small: a stamp
// with how many wait (a badge), or the next one to decide (a strip).
const { t, when } = useLang()
const toast = useToast()
const { projectId, narrow, shape } = useWatchBox()

const { data, error, refresh } = useLiveFetch<{ incidents: Incident[] }>('/api/incidents', { key: 'incidents', lazy: true }) // pushed as it changes
const list = computed(() => (data.value?.incidents ?? []).filter(x => isApproval(x) && (!projectId.value || x.project_id === projectId.value)))

const busy = ref('')
async function decide(x: Incident, what: 'approve' | 'reject') {
  busy.value = x.key + what
  try {
    await decideApproval(x, what)
    await refresh()
  } catch (e) {
    const d = alreadyDecided(e) // decided elsewhere first: said so, and the list catches up
    toast.add(d ? { title: alreadyDecidedText(d, t), color: 'info' } : { title: apiError(e), color: 'error' })
    if (d) void refresh()
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
    <!-- a badge: the stamp and the count; all clear, a green check -->
    <NuxtLink
      v-else-if="shape === 'badge' || shape === 'column'" :to="list[0]?.link ?? '/'" :title="list.map(x => x.title).join('\n') || t('watchW.approvals.none')"
      class="flex size-full flex-col items-center justify-center gap-0.5 transition hover:bg-(--ui-bg-elevated)/50"
      :class="list.length ? 'bg-(--ui-warning)/8' : ''"
    >
      <span class="relative">
        <UIcon :name="list.length ? 'i-lucide-stamp' : 'i-lucide-badge-check'" class="size-6" :class="list.length ? 'text-(--ui-warning)' : 'text-(--ui-success)'" />
        <span v-if="list.length" class="absolute -end-2.5 -top-2 grid min-w-5 place-items-center rounded-full bg-(--ui-error) px-1 text-[11px] font-semibold text-white tabular-nums">{{ list.length }}</span>
      </span>
      <span class="text-[11px] text-(--ui-text-muted)">{{ list.length ? t('watchW.approvals.waiting') : t('watchW.approvals.clear') }}</span>
    </NuxtLink>
    <WatchEmpty v-else-if="!list.length" icon="i-lucide-badge-check" :text="t('watchW.approvals.none')" />
    <!-- a strip: the next card to decide, how many after it -->
    <div v-else-if="shape === 'strip'" class="flex size-full items-center gap-2 px-2.5">
      <span class="grid size-7 shrink-0 place-items-center rounded-full bg-(--ui-error) text-xs font-semibold text-white tabular-nums">{{ list.length }}</span>
      <NuxtLink :to="list[0]!.link" class="min-w-0 flex-1 truncate text-sm hover:underline" :title="list[0]!.title">{{ list[0]!.title }}</NuxtLink>
      <UButton size="xs" icon="i-lucide-check" :label="narrow ? undefined : t('watchW.approvals.approve')" :aria-label="t('watchW.approvals.approve')" :loading="busy === list[0]!.key + 'approve'" :disabled="!!busy" @click="decide(list[0]!, 'approve')" />
    </div>
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
