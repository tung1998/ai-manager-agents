<script setup lang="ts">
// One workflow run (ADR-098): the chat that called it, its input and output,
// and what happened inside (its own chat, to read only).
import type { MessageKey } from '~/locales/vi'

const route = useRoute()
const { t, dateLocale } = useLang()
const projectId = computed(() => String(route.params.id))
const { data, error, refresh } = useLiveFetch<{ run: WorkflowRun, children: WorkflowRun[] }>(() => `/api/workflow-runs/${route.params.runId}`)
const run = computed(() => data.value?.run)
const isolated = computed(() => !!run.value?.caller_conversation_id) // older runs ran inside the chat itself
const chatTo = computed(() => run.value && ({ path: `/projects/${projectId.value}`, query: { tab: 'chat', c: run.value.caller_conversation_id || run.value.conversation_id } }))
const statusColor: Record<WorkflowRun['status'], 'info' | 'success' | 'error' | 'neutral'> = { running: 'info', done: 'success', failed: 'error', stopped: 'neutral' }
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value)
useHead({ title: () => run.value?.workflow_name ?? t('wf.section') })
</script>

<template>
  <PageShell :title="run?.workflow_name ?? t('wf.section')">
    <div>
      <UButton :to="{ path: `/projects/${projectId}`, query: { tab: 'workflows', wv: 'runs' } }" icon="i-lucide-arrow-left" size="xs" color="neutral" variant="ghost" class="-ms-2 mb-2" :label="t('wf.runs.back')" />
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :title="apiError(error)" />
    <LoadingRows v-else-if="!run" />
    <div v-else class="space-y-5">
      <div class="flex flex-wrap items-center gap-2 text-sm text-(--ui-text-muted)">
        <UBadge :color="statusColor[run.status]" variant="subtle" :label="t(`wf.status.${run.status}` as MessageKey)" />
        <span class="font-mono">/{{ run.workflow_key }}</span>
        <span>· {{ when(run.started_at) }}</span>
        <NuxtLink v-if="chatTo" :to="chatTo" class="flex items-center gap-1 hover:text-primary">
          · <UIcon name="i-lucide-message-square" class="size-4" />{{ run.caller_title || t('wf.runs.chat') }}
        </NuxtLink>
      </div>

      <UAlert
        v-if="run.parent_run_id" color="neutral" variant="subtle" icon="i-lucide-corner-left-up"
        :title="t('wf.runPage.sub', { depth: run.depth })"
      >
        <template #actions>
          <UButton size="xs" color="neutral" variant="outline" :label="t('wf.runPage.parent')" :to="`/projects/${projectId}/workflows/runs/${run.parent_run_id}`" />
        </template>
      </UAlert>

      <WorkflowRunCard :run="run" detail @updated="refresh()" />

      <section v-if="data?.children?.length" class="space-y-1.5">
        <h2 class="text-sm font-semibold">{{ t('wf.runPage.children', { n: data.children.length }) }}</h2>
        <ul class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
          <li v-for="c in data.children" :key="c.id" class="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
            <UIcon name="i-lucide-corner-down-right" class="size-4 text-(--ui-text-muted)" />
            <NuxtLink :to="`/projects/${projectId}/workflows/runs/${c.id}`" class="font-medium hover:text-primary">{{ c.workflow_name }}</NuxtLink>
            <UBadge :color="statusColor[c.status]" variant="subtle" size="sm" :label="t(`wf.status.${c.status}` as MessageKey)" />
            <span class="min-w-0 flex-1 truncate text-xs text-(--ui-text-muted)">{{ c.input }}</span>
            <span class="shrink-0 text-xs text-(--ui-text-muted)">${{ c.cost_usd.toFixed(3) }}</span>
          </li>
        </ul>
      </section>

      <section class="space-y-1.5">
        <h2 class="text-sm font-semibold">{{ t('wf.runs.input') }}</h2>
        <p class="whitespace-pre-wrap rounded-lg bg-(--ui-bg-elevated) px-3 py-2 text-sm">{{ run.input || t('wf.runPage.noInput') }}</p>
      </section>
      <section class="space-y-1.5">
        <h2 class="text-sm font-semibold">{{ t('wf.runs.output') }}</h2>
        <p v-if="run.error" class="whitespace-pre-wrap text-sm text-(--ui-error)">{{ run.error }}</p>
        <!-- eslint-disable-next-line vue/no-v-html -->
        <div v-else-if="run.result" class="markdown rounded-lg border border-(--ui-border) px-3 py-2 text-sm" v-html="renderMarkdown(run.result)" />
        <p v-else class="text-sm text-(--ui-text-muted)">{{ run.status === 'running' ? t('wf.runPage.notYet') : t('wf.runPage.noOutput') }}</p>
      </section>
      <section class="space-y-2">
        <div class="flex items-center gap-2">
          <h2 class="text-sm font-semibold">{{ t('wf.runPage.content') }}</h2>
          <UTooltip :text="t('wf.runPage.contentInfo')"><UIcon name="i-lucide-info" class="size-4 text-(--ui-text-muted)" /></UTooltip>
        </div>
        <RunTranscript v-if="isolated" :project-id="projectId" :conversation-id="run.conversation_id" class="rounded-lg border border-(--ui-border) p-3" />
        <p v-else class="text-sm text-(--ui-text-muted)">
          {{ t('wf.runPage.inChat') }} <NuxtLink v-if="chatTo" :to="chatTo" class="text-primary">{{ t('wf.runs.openChat') }}</NuxtLink>
        </p>
      </section>
    </div>
  </PageShell>
</template>
