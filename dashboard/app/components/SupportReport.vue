<script setup lang="ts">
// How support went (ADR-174): tickets, first answer, SLA, who answered
// (the AI alone, approved, edited, a person), and what got no answer.
const props = defineProps<{ projectId: string }>()
const { t } = useLang()
const days = ref(7)
const { data } = useLiveFetch<SupportReport>(() => `/api/projects/${props.projectId}/support/report?days=${days.value}`, { lazy: true })
const tiles = computed(() => {
  const r = data.value
  if (!r) return []
  return [
    { label: t('sup.r.tickets'), value: r.tickets },
    { label: t('sup.r.answered'), value: r.answered },
    { label: t('sup.r.firstResponse'), value: r.answered ? `${Math.round(r.first_response_minutes)}′` : '—' },
    { label: t('sup.r.sla'), value: r.sla_breached },
    { label: t('sup.r.escalated'), value: r.escalated }
  ]
})
const replies = computed(() => {
  const r = data.value
  if (!r) return []
  return [
    { label: t('sup.r.auto'), value: r.ai_auto }, { label: t('sup.r.approved'), value: r.ai_approved },
    { label: t('sup.r.edited'), value: r.ai_edited }, { label: t('sup.r.person'), value: r.by_person }, { label: t('sup.r.failed'), value: r.ai_failed }
  ]
})
</script>

<template>
  <div class="max-w-4xl space-y-3">
    <USelect v-model="days" :items="[{ label: t('sup.r.days', { n: 7 }), value: 7 }, { label: t('sup.r.days', { n: 30 }), value: 30 }, { label: t('sup.r.days', { n: 90 }), value: 90 }]" size="sm" class="w-40" />
    <LoadingRows v-if="!data" :n="3" />
    <template v-else>
      <div class="grid grid-cols-2 gap-2 sm:grid-cols-5">
        <div v-for="x in tiles" :key="x.label" class="rounded-lg border border-(--ui-border) p-3">
          <p class="text-xs text-(--ui-text-muted)">{{ x.label }}</p>
          <p class="text-xl font-semibold tabular-nums">{{ x.value }}</p>
        </div>
      </div>
      <div class="grid gap-3 sm:grid-cols-2">
        <div class="rounded-lg border border-(--ui-border) p-3 text-sm">
          <p class="mb-2 font-medium">{{ t('sup.r.replies') }}</p>
          <div v-for="x in replies" :key="x.label" class="flex justify-between py-0.5"><span class="text-(--ui-text-muted)">{{ x.label }}</span><span class="tabular-nums">{{ x.value }}</span></div>
        </div>
        <div class="rounded-lg border border-(--ui-border) p-3 text-sm">
          <p class="mb-2 font-medium">{{ t('sup.r.cases') }}</p>
          <div v-for="(n, k) in data.by_case" :key="k" class="flex justify-between py-0.5"><span class="text-(--ui-text-muted)">{{ k === '-' ? t('sup.r.noCase') : k }}</span><span class="tabular-nums">{{ n }}</span></div>
        </div>
      </div>
      <div v-if="data.unanswered.length" class="rounded-lg border border-(--ui-border) p-3 text-sm">
        <p class="mb-2 font-medium">{{ t('sup.r.unanswered') }} <InfoTip :text="t('sup.r.unansweredHelp')" /></p>
        <ul class="list-disc space-y-1 ps-5 text-(--ui-text-toned)">
          <li v-for="(u, i) in data.unanswered" :key="i">{{ u }}</li>
        </ul>
      </div>
    </template>
  </div>
</template>
