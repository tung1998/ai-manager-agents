<script setup lang="ts">
// A workflow run in its chat (ADR-098): who fills each role and how far they
// got, its gates, turns and cost; Stop ends it (and every answer of the chat).
import type { MessageKey } from '~/locales/vi'

const props = defineProps<{ run: WorkflowRun }>()
const emit = defineEmits<{ updated: [WorkflowRun] }>()
const toast = useToast()
const { t, dateLocale } = useLang()

const statusColor: Record<WorkflowRun['status'], 'info' | 'success' | 'error' | 'neutral'> = { running: 'info', done: 'success', failed: 'error', stopped: 'neutral' }
const roleIcon: Record<RunRole['status'], string> = { idle: 'i-lucide-circle-dashed', working: 'i-lucide-loader-circle', done: 'i-lucide-circle-check', failed: 'i-lucide-circle-x' }
const roleColor: Record<RunRole['status'], string> = { idle: 'text-(--ui-text-dimmed)', working: 'text-(--ui-info) animate-spin', done: 'text-(--ui-success)', failed: 'text-(--ui-error)' }
const gateColor: Record<RunGate['status'], 'neutral' | 'warning' | 'success' | 'error'> = { open: 'neutral', waiting: 'warning', passed: 'success', failed: 'error' }
const usd = (n: number) => `$${n.toFixed(n < 1 ? 3 : 2)}`
const when = (d: string) => new Date(d).toLocaleTimeString(dateLocale.value, { hour: '2-digit', minute: '2-digit', second: '2-digit' })

const stopping = ref(false)
async function stop() {
  stopping.value = true
  try {
    const res = await $fetch<{ run: WorkflowRun }>(`/api/workflow-runs/${props.run.id}/stop`, { method: 'POST', body: {} })
    emit('updated', res.run)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    stopping.value = false
  }
}
</script>

<template>
  <div class="space-y-2 rounded-lg border border-(--ui-border) px-3 py-2.5 text-sm">
    <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
      <UIcon name="i-lucide-workflow" class="size-4 shrink-0 text-primary" />
      <span class="font-medium">{{ run.workflow_name }}</span>
      <span class="font-mono text-xs text-(--ui-text-muted)">/{{ run.workflow_key }}</span>
      <UBadge :color="statusColor[run.status]" variant="subtle" size="sm" :label="t(`wf.status.${run.status}` as MessageKey)" :icon="run.status === 'running' ? 'i-lucide-loader-circle' : undefined" :ui="{ leadingIcon: 'animate-spin' }" />
      <span class="ms-auto flex items-center gap-2 text-xs text-(--ui-text-muted)">
        <span :title="t('wf.run.turnsInfo')">{{ run.max_turns ? t('wf.run.turnsOf', { n: run.turns, max: run.max_turns }) : t('wf.run.turns', { n: run.turns }) }}</span>
        <span>· {{ usd(run.cost_usd) }}</span>
        <UButton v-if="run.status === 'running'" size="xs" color="neutral" variant="outline" icon="i-lucide-square" :label="t('chat.stop')" :loading="stopping" @click="stop" />
      </span>
    </div>
    <p class="text-xs text-(--ui-text-muted)">
      {{ t('wf.run.coordinator', { name: run.coordinator_name }) }}<template v-if="run.input"> · <span class="italic">{{ run.input }}</span></template>
    </p>

    <ul v-if="run.roles.length" class="divide-y divide-(--ui-border) rounded-md border border-(--ui-border)">
      <li v-for="r in run.roles" :key="r.role" class="flex items-center gap-2 px-2.5 py-1.5 text-xs">
        <UIcon :name="roleIcon[r.status]" class="size-4 shrink-0" :class="roleColor[r.status]" :title="t(`wf.roleStatus.${r.status}` as MessageKey)" />
        <span class="font-medium">{{ r.name || r.role }}</span>
        <UTooltip :text="t(`wf.access.${r.access}` as MessageKey)">
          <UIcon :name="accessIcon[r.access] ?? 'i-lucide-eye'" class="size-3.5 text-(--ui-text-muted)" />
        </UTooltip>
        <span class="min-w-0 truncate" :class="r.agent_name ? '' : 'text-(--ui-text-dimmed)'">→ {{ r.agent_name || t('wf.unbound') }}</span>
        <span class="ms-auto shrink-0 text-(--ui-text-muted)">
          {{ t('wf.run.turns', { n: r.turns }) }}<template v-if="r.rounds"> · {{ t('wf.run.rounds', { n: r.rounds }) }}</template> · {{ usd(r.cost_usd) }}
        </span>
      </li>
    </ul>

    <div v-if="run.gates.length" class="flex flex-wrap items-center gap-1.5 text-xs">
      <span class="text-(--ui-text-muted)">{{ t('wf.gates') }}</span>
      <UBadge
        v-for="g in run.gates" :key="g.key" :color="gateColor[g.status]" variant="subtle" size="sm"
        :icon="g.kind === 'approve' ? 'i-lucide-user-check' : 'i-lucide-test-tube'"
        :label="`${g.name} · ${t(`wf.gateStatus.${g.status}` as MessageKey)}`" :title="g.detail || undefined"
      />
    </div>

    <p v-if="run.error" class="whitespace-pre-wrap text-xs text-(--ui-error)">{{ run.error }}</p>
    <p v-else-if="run.result && run.status !== 'running'" class="line-clamp-3 whitespace-pre-wrap text-xs text-(--ui-text-muted)" :title="run.result">{{ run.result }}</p>

    <details v-if="run.log?.length" class="text-xs text-(--ui-text-muted)">
      <summary class="cursor-pointer">{{ t('wf.run.log', { n: run.log.length }) }}</summary>
      <ul class="mt-1 space-y-0.5 ps-4">
        <li v-for="(l, i) in run.log" :key="i"><span class="font-mono text-(--ui-text-dimmed)">{{ when(l.at) }}</span> {{ l.text }}</li>
      </ul>
    </details>
  </div>
</template>
