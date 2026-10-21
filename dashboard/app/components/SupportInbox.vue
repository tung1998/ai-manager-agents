<script setup lang="ts">
// The Inbox (ADR-171): tickets on the left, the one picked on the right
// (SupportTicketView).
const props = defineProps<{ projectId: string }>()
const { t, when } = useLang()
const route = useRoute()

const status = ref<string>('')
const statusItems = computed(() => [
  { label: t('sup.st.all'), value: '' },
  ...(['waiting_approval', 'open', 'new', 'waiting_customer', 'resolved', 'closed'] as TicketStatus[]).map(s => ({ label: t(`sup.st.${s}` as 'sup.st.new'), value: s }))
])
const base = computed(() => `/api/projects/${props.projectId}/support`)
const { data, error } = useLiveFetch<{ tickets: SupportTicket[] }>(() => `${base.value}/tickets${status.value ? `?status=${status.value}` : ''}`, { lazy: true })
const tickets = computed(() => data.value?.tickets ?? [])

const selected = computed({
  get: () => String(route.query.ticket ?? ''),
  set: v => navigateTo({ query: { ...route.query, ticket: v || undefined } }, { replace: true })
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-3 md:flex-row">
    <!-- tickets -->
    <div class="flex min-h-0 flex-col rounded-lg border border-(--ui-border) md:w-80 md:shrink-0" :class="selected ? 'hidden md:flex' : ''">
      <div class="border-b border-(--ui-border) p-2">
        <USelect v-model="status" :items="statusItems" size="sm" class="w-full" />
      </div>
      <UAlert v-if="error" color="error" variant="subtle" :title="apiError(error)" class="m-2" />
      <LoadingRows v-else-if="!data" :n="4" />
      <p v-else-if="!tickets.length" class="p-4 text-sm text-(--ui-text-muted)">{{ t('sup.noTickets') }}</p>
      <ul v-else class="min-h-0 flex-1 divide-y divide-(--ui-border) overflow-y-auto">
        <li v-for="x in tickets" :key="x.id">
          <button type="button" class="w-full px-3 py-2 text-left hover:bg-(--ui-bg-elevated)" :class="x.id === selected ? 'bg-(--ui-bg-elevated)' : ''" @click="selected = x.id">
            <p class="truncate text-sm font-medium">{{ x.subject || x.id }}</p>
            <div class="mt-0.5 flex items-center gap-2 text-xs text-(--ui-text-muted)">
              <UBadge :color="ticketColor[x.status]" variant="subtle" size="sm" :label="t(`sup.st.${x.status}` as 'sup.st.new')" />
              <UBadge v-if="x.case_type" color="neutral" variant="outline" size="sm" :label="x.case_type" />
              <UIcon v-if="x.tags?.includes('human')" name="i-lucide-user-round" class="size-3.5" :aria-label="t('sup.human')" />
              <UIcon v-if="x.sla_breached" name="i-lucide-alarm-clock-off" class="size-3.5 text-(--ui-error)" :aria-label="t('sup.slaBreached')" />
              <span class="ms-auto">{{ when(x.updated_at) }}</span>
            </div>
          </button>
        </li>
      </ul>
    </div>

    <!-- one ticket -->
    <SupportTicketView v-if="selected" :key="selected" :project-id="props.projectId" :ticket-id="selected" class="flex-1" @back="selected = ''" />
    <div v-else class="hidden flex-1 items-center justify-center rounded-lg border border-dashed border-(--ui-border) text-sm text-(--ui-text-muted) md:flex">
      {{ t('sup.pick') }}
    </div>
  </div>
</template>
