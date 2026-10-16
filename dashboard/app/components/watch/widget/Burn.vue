<script setup lang="ts">
// A project's Burn at a glance: on or off, how long it has left, its pieces
// by column, what it spent, what it is doing now. Small: its flame and the
// pieces being done (a badge), or the counts in a row (a strip).
interface BurnLite { state: 'running' | 'stopped' | 'waiting_limit' | 'blocked' | 'draining', ends_at: string | null, waiting_until?: string, run_branch?: string }
interface ItemLite { id: string, title: string, status: string, cost_usd: number, run_branch: string }
const { t, when } = useLang()
const { projectId, shape } = useWatchBox()
const { data, error, refresh } = useLiveFetch<{ burn: BurnLite, items: ItemLite[] }>(() => `/api/projects/${projectId.value}/burn`, { lazy: true, server: false })
const burn = computed(() => data.value?.burn)
const now = useNow()

const on = computed(() => !!burn.value && burn.value.state !== 'stopped')
const stateColor = computed(() => ({ running: 'warning', blocked: 'error', waiting_limit: 'info', draining: 'info', stopped: 'neutral' } as const)[burn.value?.state ?? 'stopped'])
const toneText: Record<string, string> = { warning: 'text-(--ui-warning)', error: 'text-(--ui-error)', info: 'text-(--ui-info)', neutral: 'text-(--ui-text-dimmed)' }
// this run's pieces (a stopped Burn: the last run's), and the pieces still open
const runItems = computed(() => {
  const all = data.value?.items ?? []
  const run = burn.value?.run_branch
  return run ? all.filter(i => i.run_branch === run || !i.run_branch) : all
})
const count = (...s: string[]) => runItems.value.filter(i => s.includes(i.status)).length
const counts = computed(() => [
  { key: 'found', n: count('found'), label: t('burn.col.found'), cls: 'text-(--ui-text)' },
  { key: 'doing', n: count('doing', 'review', 'queued'), label: t('burn.col.doing'), cls: 'text-(--ui-warning)' },
  { key: 'done', n: count('done'), label: t('burn.col.done'), cls: 'text-(--ui-success)' },
  { key: 'other', n: count('failed', 'skipped'), label: t('burn.col.other'), cls: 'text-(--ui-text-muted)' }
])
const doing = computed(() => runItems.value.filter(i => i.status === 'doing' || i.status === 'review'))
const cost = computed(() => runItems.value.reduce((a, i) => a + (i.cost_usd || 0), 0))
// when it turns off, or waits until
const left = computed(() => {
  const b = burn.value
  if (!b || !on.value) return ''
  if (b.state === 'waiting_limit' && b.waiting_until) return t('watchW.burn.waits', { left: fmtLeft(Date.parse(b.waiting_until) - now.value) })
  if (b.ends_at) return t('watchW.burn.left', { left: fmtLeft(Date.parse(b.ends_at) - now.value) })
  return ''
})
const link = computed(() => `/projects/${projectId.value}?tab=burn`)
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto">
    <div v-if="!data && !error" class="p-2"><LoadingRows :n="3" /></div>
    <div v-else-if="error" class="p-3"><UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" /></div>
    <!-- a badge: its flame, the pieces being done -->
    <NuxtLink
      v-else-if="shape === 'badge' || shape === 'column'" :to="link" :title="burn ? t(`burn.state.${burn.state}`) : ''"
      class="flex size-full flex-col items-center justify-center gap-1 p-1"
    >
      <UIcon name="i-lucide-flame" class="size-7" :class="[toneText[stateColor], on && 'animate-pulse']" />
      <span class="text-xs font-semibold tabular-nums">{{ on ? counts[1]!.n : t('watchW.burn.off') }}</span>
      <span v-if="shape === 'column' && left" class="text-[10px] text-(--ui-text-muted) tabular-nums">{{ left }}</span>
    </NuxtLink>
    <!-- a strip: its state, then the counts side by side -->
    <NuxtLink v-else-if="shape === 'strip'" :to="link" class="flex size-full items-center gap-3 overflow-hidden px-2.5">
      <UIcon name="i-lucide-flame" class="size-5 shrink-0" :class="toneText[stateColor]" />
      <span class="shrink-0 text-xs" :class="toneText[stateColor]">{{ burn ? t(`burn.state.${burn.state}`) : '' }}</span>
      <span v-for="c in counts" :key="c.key" class="flex shrink-0 items-baseline gap-1 text-xs" :title="c.label">
        <span class="font-semibold tabular-nums" :class="c.cls">{{ c.n }}</span><span class="truncate text-(--ui-text-dimmed)">{{ c.label }}</span>
      </span>
      <span v-if="left" class="ms-auto shrink-0 text-xs text-(--ui-text-muted) tabular-nums">{{ left }}</span>
    </NuxtLink>
    <div v-else-if="burn" class="flex h-full flex-col gap-2 p-2.5">
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-flame" class="size-5 shrink-0" :class="[toneText[stateColor], on && 'animate-pulse']" />
        <UBadge :label="t(`burn.state.${burn.state}`)" :color="stateColor" variant="subtle" size="sm" />
        <span v-if="left" class="truncate text-xs text-(--ui-text-muted) tabular-nums" :title="burn.ends_at ? when(burn.ends_at) : undefined">{{ left }}</span>
        <UButton :to="link" size="xs" color="neutral" variant="ghost" icon="i-lucide-arrow-up-right" class="ms-auto" :aria-label="t('watchW.burn.open')" :title="t('watchW.burn.open')" />
      </div>
      <div class="grid grid-cols-4 gap-1.5">
        <div v-for="c in counts" :key="c.key" class="min-w-0 rounded-md bg-(--ui-bg-elevated)/60 px-1.5 py-1 text-center">
          <p class="text-lg font-semibold leading-tight tabular-nums" :class="c.cls">{{ c.n }}</p>
          <p class="truncate text-[10px] text-(--ui-text-muted)" :title="c.label">{{ c.label }}</p>
        </div>
      </div>
      <p class="text-xs text-(--ui-text-muted)">{{ t('watchW.burn.spent', { usd: cost.toFixed(2) }) }}</p>
      <ul v-if="shape === 'panel' && doing.length" class="min-h-0 flex-1 space-y-1 overflow-y-auto">
        <li v-for="it in doing" :key="it.id" class="flex items-center gap-2 text-sm">
          <UIcon v-if="it.status === 'doing'" name="i-lucide-loader-circle" class="size-4 shrink-0 animate-spin text-(--ui-warning)" />
          <UIcon v-else name="i-lucide-scan-eye" class="size-4 shrink-0 text-(--ui-info)" :title="t('burn.review.waiting')" />
          <span class="truncate" :title="it.title">{{ it.title }}</span>
        </li>
      </ul>
    </div>
  </div>
</template>
