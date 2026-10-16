<script setup lang="ts">
// A project's Burn work, managed from the board: a quest added in one line,
// the open pieces (being done first, then waiting, then found) with Ưu tiên
// and Bỏ qua; the finished ones of this run too when turned on in ⚙.
interface ItemLite { id: string, title: string, kind: string, status: string, priority: number, run_branch: string, updated_at: string }
const { t } = useLang()
const toast = useToast()
const { projectId, config, narrow } = useWatchBox()
const base = computed(() => `/api/projects/${projectId.value}/burn`)
const { data, error, refresh } = useLiveFetch<{ burn: { run_branch?: string }, items: ItemLite[] }>(base, { lazy: true, server: false })

const finished = ['done', 'failed', 'skipped']
const rank: Record<string, number> = { doing: 0, review: 1, queued: 2, paused: 3, found: 4 }
const open = computed(() => (data.value?.items ?? [])
  .filter(i => !finished.includes(i.status))
  .sort((a, b) => (rank[a.status] ?? 9) - (rank[b.status] ?? 9) || b.priority - a.priority))
const closed = computed(() => {
  const run = data.value?.burn.run_branch
  if (!config.value.done || !run) return []
  return (data.value?.items ?? []).filter(i => i.run_branch === run && finished.includes(i.status))
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at)).slice(0, 20)
})
const list = computed(() => [...open.value, ...closed.value])

const statusIcon: Record<string, [string, string]> = {
  doing: ['i-lucide-loader-circle', 'animate-spin text-(--ui-warning)'],
  review: ['i-lucide-scan-eye', 'text-(--ui-info)'],
  queued: ['i-lucide-clock', 'text-(--ui-text-muted)'],
  paused: ['i-lucide-circle-pause', 'text-(--ui-text-muted)'],
  found: ['i-lucide-circle-dashed', 'text-(--ui-text-dimmed)'],
  done: ['i-lucide-circle-check', 'text-(--ui-success)'],
  failed: ['i-lucide-circle-x', 'text-(--ui-error)'],
  skipped: ['i-lucide-circle-slash', 'text-(--ui-text-dimmed)']
}
const statusLabel = (s: string) => ({ doing: t('burn.col.doing'), review: t('burn.review.waiting'), paused: t('burn.col.paused'), found: t('burn.col.found'), done: t('burn.col.done') } as Record<string, string>)[s] ?? s

// a quest in one line: done as given before the Burn looks for more
const title = ref('')
const adding = ref(false)
async function addQuest() {
  const v = title.value.trim()
  if (!v || adding.value) return
  adding.value = true
  try {
    await $fetch(`${base.value}/quests`, { method: 'POST', body: { title: v, detail: '' } })
    title.value = ''
    toast.add({ title: t('burn.quest.added'), color: 'success' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    adding.value = false
  }
}

const acting = ref<Record<string, string>>({})
async function act(it: ItemLite, action: 'first' | 'skip') {
  acting.value = { ...acting.value, [it.id]: action }
  try {
    await $fetch(`/api/burn-items/${it.id}/${action}`, { method: 'POST' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    const { [it.id]: _, ...rest } = acting.value
    acting.value = rest
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <form class="flex shrink-0 items-center gap-1 border-b border-(--ui-border) p-1.5" @submit.prevent="addQuest">
      <UInput v-model="title" size="sm" :placeholder="t('watchW.burnwork.add')" :disabled="adding || !data" class="min-w-0 flex-1" :ui="{ base: 'w-full' }" />
      <UButton type="submit" size="sm" color="neutral" variant="ghost" icon="i-lucide-plus" :loading="adding" :disabled="!title.trim()" :aria-label="t('burn.quest.add')" :title="t('burn.quest.add')" />
    </form>
    <div class="min-h-0 flex-1 overflow-y-auto">
      <div v-if="!data && !error" class="p-2"><LoadingRows :n="4" /></div>
      <div v-else-if="error" class="p-3"><UAlert color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" /></div>
      <WatchEmpty v-else-if="!list.length" icon="i-lucide-list-todo" :text="t('watchW.burnwork.none')" />
      <ul v-else class="divide-y divide-(--ui-border)">
        <li v-for="it in list" :key="it.id" class="group/row flex items-center gap-2 px-2.5 py-1.5" :class="finished.includes(it.status) && 'opacity-60'">
          <UIcon :name="statusIcon[it.status]?.[0] ?? 'i-lucide-circle'" class="size-4 shrink-0" :class="statusIcon[it.status]?.[1]" :title="statusLabel(it.status)" />
          <NuxtLink :to="`/projects/${projectId}?tab=burn`" class="min-w-0 flex-1 truncate text-sm hover:underline" :title="it.title">{{ it.title }}</NuxtLink>
          <UBadge v-if="it.kind && !narrow" :label="t(`burn.kind.${it.kind}` as 'burn.kind.bug')" color="neutral" variant="subtle" size="sm" class="shrink-0" />
          <span class="flex shrink-0 items-center opacity-0 transition group-hover/row:opacity-100 focus-within:opacity-100 pointer-coarse:opacity-100" :class="acting[it.id] && 'opacity-100'">
            <UButton
              v-if="['found', 'skipped', 'failed'].includes(it.status)" size="xs" color="neutral" variant="ghost" icon="i-lucide-arrow-up-to-line"
              :aria-label="t('burn.first')" :title="t('burn.first')" :loading="acting[it.id] === 'first'" :disabled="!!acting[it.id]" @click="act(it, 'first')"
            />
            <UButton
              v-if="['found', 'queued', 'paused', 'review'].includes(it.status)" size="xs" color="neutral" variant="ghost" icon="i-lucide-skip-forward"
              :aria-label="t('burn.skip')" :title="t('burn.skip')" :loading="acting[it.id] === 'skip'" :disabled="!!acting[it.id]" @click="act(it, 'skip')"
            />
          </span>
        </li>
      </ul>
    </div>
  </div>
</template>
