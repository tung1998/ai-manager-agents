<script setup lang="ts">
// The runs of a project's workflows (newest first): the chat that called
// each, what it was asked and what it gave back; one opens its page.
import type { MessageKey } from '~/locales/vi'

const props = defineProps<{ projectId: string, workflows: ProjectWorkflow[] }>()
const { t, dateLocale } = useLang()
const route = useRoute()
const router = useRouter()

const ALL = '_'
const pick = ref(typeof route.query.w === 'string' ? route.query.w : ALL)
watch(pick, w => router.replace({ query: { ...route.query, w: w === ALL ? undefined : w } }))
watch(() => route.query.w, w => { pick.value = typeof w === 'string' ? w : ALL })
const items = computed(() => [{ label: t('wf.runs.all'), value: ALL }, ...props.workflows.map(w => ({ label: w.name, value: w.id }))])
const { data, error, pending } = useLiveFetch<{ runs: WorkflowRun[] }>(
  () => `/api/projects/${props.projectId}/workflow-runs?limit=100${pick.value === ALL ? '' : `&workflow=${pick.value}`}`, { lazy: true })
const runs = computed(() => data.value?.runs ?? [])

const statusColor: Record<WorkflowRun['status'], 'info' | 'success' | 'error' | 'neutral'> = { running: 'info', done: 'success', failed: 'error', stopped: 'neutral' }
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
const took = (r: WorkflowRun) => {
  const s = Math.round(((r.finished_at ? new Date(r.finished_at) : new Date()).getTime() - new Date(r.started_at).getTime()) / 1000)
  return s < 60 ? `${s}s` : s < 3600 ? `${Math.round(s / 60)}m` : `${(s / 3600).toFixed(1)}h`
}
const open = (r: WorkflowRun) => `/projects/${props.projectId}/workflows/runs/${r.id}`
const chatOf = (r: WorkflowRun) => ({ path: `/projects/${props.projectId}`, query: { tab: 'chat', c: r.caller_conversation_id || r.conversation_id } })
</script>

<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-center gap-2">
      <USelect v-model="pick" :items="items" size="sm" class="w-56" :aria-label="t('wf.runs.filter')" />
      <p class="text-sm text-(--ui-text-muted)">{{ t('wf.runs.count', { n: runs.length }) }}</p>
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :title="apiError(error)" />
    <LoadingRows v-else-if="pending && !data" />
    <div v-else-if="!runs.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
      <UIcon name="i-lucide-history" class="mx-auto size-8 text-(--ui-text-dimmed)" />
      <p class="mt-2 font-medium">{{ t('wf.runs.empty') }}</p>
      <p class="text-sm text-(--ui-text-muted)">{{ t('wf.runs.emptyHint') }}</p>
    </div>
    <ul v-else class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
      <li v-for="r in runs" :key="r.id" class="relative px-3 py-2.5 hover:bg-(--ui-bg-elevated)/50">
        <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
          <UIcon v-if="r.parent_run_id" name="i-lucide-corner-down-right" class="size-4 text-(--ui-text-muted)" :title="t('wf.runs.sub', { depth: r.depth })" />
          <NuxtLink :to="open(r)" class="font-medium after:absolute after:inset-0 hover:text-primary">{{ r.workflow_name }}</NuxtLink>
          <UBadge :color="statusColor[r.status]" variant="subtle" size="sm" :label="t(`wf.status.${r.status}` as MessageKey)" />
          <NuxtLink :to="chatOf(r)" class="relative z-10 flex min-w-0 items-center gap-1 text-xs text-(--ui-text-muted) hover:text-primary" :title="t('wf.runs.openChat')">
            <UIcon name="i-lucide-message-square" class="size-3.5 shrink-0" /><span class="max-w-56 truncate">{{ r.caller_title || t('wf.runs.chat') }}</span>
          </NuxtLink>
          <span class="ms-auto shrink-0 text-xs text-(--ui-text-muted)">{{ when(r.started_at) }} · {{ took(r) }} · ${{ r.cost_usd.toFixed(3) }}</span>
        </div>
        <p v-if="r.input" class="mt-1 line-clamp-1 text-xs"><span class="text-(--ui-text-muted)">{{ t('wf.runs.input') }}:</span> {{ r.input }}</p>
        <p v-if="r.error" class="mt-0.5 line-clamp-1 text-xs text-(--ui-error)">{{ r.error }}</p>
        <p v-else-if="r.result" class="mt-0.5 line-clamp-2 text-xs text-(--ui-text-muted)"><span>{{ t('wf.runs.output') }}:</span> {{ r.result }}</p>
      </li>
    </ul>
  </div>
</template>
