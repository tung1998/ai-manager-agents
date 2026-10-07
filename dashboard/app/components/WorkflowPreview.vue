<script setup lang="ts">
// What a workflow file says, as the office reads it: its roles and their
// access, what runs together, limits, the brief, gates and vote.
import type { MessageKey } from '~/locales/vi'

defineProps<{ def: WorkflowDef }>()
const { t } = useLang()
// the brief's parts the office knows (internal/workflow BriefFields)
const BRIEF = ['outcome', 'question', 'context', 'constraints', 'current_option', 'tried', 'files', 'done_when', 'must_not']
const briefLabel = (k: string) => BRIEF.includes(k) ? t(`wf.brief.${k}` as MessageKey) : k
</script>

<template>
  <div class="space-y-3 text-sm">
    <div>
      <p class="flex flex-wrap items-center gap-2">
        <span class="font-semibold">{{ def.name }}</span>
        <span class="font-mono text-xs text-(--ui-text-muted)">/{{ def.key }}</span>
        <UBadge v-if="def.strict" color="warning" variant="subtle" size="sm" :label="t('wf.strict')" :title="t('wf.strictInfo')" />
      </p>
      <p v-if="def.description" class="text-xs text-(--ui-text-muted)">{{ def.description }}</p>
      <p v-if="def.input" class="text-xs"><span class="text-(--ui-text-muted)">{{ t('wf.input') }}:</span> {{ def.input }}</p>
    </div>

    <div class="space-y-1">
      <p class="text-xs font-medium text-(--ui-text-muted)">{{ t('wf.roles', { n: def.roles.length }) }}</p>
      <ul class="divide-y divide-(--ui-border) rounded-md border border-(--ui-border)">
        <li v-for="r in def.roles" :key="r.key" class="flex flex-wrap items-center gap-x-2 gap-y-0.5 px-2.5 py-1.5 text-xs">
          <UTooltip :text="t(`wf.access.${r.access}` as MessageKey)">
            <UIcon :name="accessIcon[r.access] ?? 'i-lucide-eye'" class="size-4 text-primary" />
          </UTooltip>
          <span class="font-medium">{{ r.name }}</span>
          <span class="font-mono text-(--ui-text-muted)">{{ r.key }}</span>
          <span v-if="r.hint" class="text-(--ui-text-muted)">· {{ r.hint }}</span>
          <UBadge v-if="r.differ_from?.length" class="ms-auto" color="neutral" variant="soft" size="sm" icon="i-lucide-split" :label="t('wf.differFrom', { roles: r.differ_from.join(', ') })" :title="t('wf.differFromInfo')" />
        </li>
      </ul>
    </div>

    <div class="flex flex-wrap gap-1.5 text-xs">
      <UBadge v-for="(g, i) in def.parallel ?? []" :key="`p${i}`" color="neutral" variant="soft" size="sm" icon="i-lucide-columns-2" :label="t('wf.parallel', { roles: g.join(' + ') })" />
      <UBadge v-if="def.limits.rounds" color="neutral" variant="outline" size="sm" :label="t('wf.limit.rounds', { n: def.limits.rounds })" :title="t('wf.limit.roundsInfo')" />
      <UBadge v-if="def.limits.turns" color="neutral" variant="outline" size="sm" :label="t('wf.limit.turns', { n: def.limits.turns })" :title="t('wf.limit.turnsInfo')" />
      <UBadge v-if="def.limits.timeout" color="neutral" variant="outline" size="sm" icon="i-lucide-timer" :label="def.limits.timeout" />
      <UBadge v-if="def.limits.budget_usd" color="neutral" variant="outline" size="sm" icon="i-lucide-wallet" :label="`$${def.limits.budget_usd}`" />
    </div>

    <div v-if="def.brief?.length" class="space-y-1">
      <p class="text-xs font-medium text-(--ui-text-muted)">{{ t('wf.briefTitle') }}</p>
      <div class="flex flex-wrap gap-1">
        <UBadge v-for="b in def.brief" :key="b" color="neutral" variant="subtle" size="sm" :label="briefLabel(b)" />
      </div>
    </div>

    <div v-if="def.gates?.length" class="space-y-1">
      <p class="text-xs font-medium text-(--ui-text-muted)">{{ t('wf.gates') }}</p>
      <ul class="space-y-0.5 text-xs">
        <li v-for="g in def.gates" :key="g.key" class="flex items-center gap-1.5">
          <UIcon :name="g.kind === 'approve' ? 'i-lucide-user-check' : 'i-lucide-test-tube'" class="size-3.5 text-primary" />
          <span class="font-medium">{{ g.name }}</span>
          <span class="text-(--ui-text-muted)">· {{ t(`wf.gate.${g.kind}` as MessageKey) }}</span>
          <code v-if="g.command" class="rounded bg-(--ui-bg-elevated) px-1 font-mono">{{ g.command }}</code>
          <UBadge v-if="g.required" color="warning" variant="subtle" size="sm" :label="t('wf.required')" />
        </li>
      </ul>
    </div>

    <p v-if="def.vote" class="flex items-center gap-1.5 text-xs">
      <UIcon name="i-lucide-vote" class="size-3.5 text-primary" />
      {{ t('wf.vote', { quorum: def.vote.quorum, roles: (def.vote.roles ?? []).join(', ') }) }}
      <template v-if="def.vote.veto?.length"> · {{ t('wf.veto', { roles: def.vote.veto.join(', ') }) }}</template>
    </p>
  </div>
</template>
