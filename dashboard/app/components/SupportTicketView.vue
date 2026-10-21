<script setup lang="ts">
// One ticket of the Inbox (ADR-171): its history; an AI answer waiting is
// approved here (edited or not); a person answers by hand or closes it.
const props = defineProps<{ projectId: string, ticketId: string }>()
const emit = defineEmits<{ back: [] }>()
const { t, when } = useLang()
const toast = useToast()
const { isAdmin } = useAuth()
const base = computed(() => `/api/projects/${props.projectId}/support`)
const { data: shown } = useLiveFetch<{ ticket: SupportTicket, events: SupportEvent[], customer?: DataRecord }>(() => `${base.value}/tickets/${props.ticketId}`, { lazy: true })
// what a person reads: the messages and answers; drafts no longer waiting fold away
const lines = computed(() => (shown.value?.events ?? []).filter(e => e.kind !== 'ai_draft' || e.state === 'pending' || e.state === 'failed'))
const statusText = (e: SupportEvent) => e.body === 'sla_breached' ? t('sup.slaBreached') : t('sup.escalated')

const edits = reactive<Record<string, string>>({})
const busy = ref(false)
async function act(path: string, body?: object, done?: string) {
  if (busy.value) return
  busy.value = true
  try {
    await $fetch(`${base.value}/${path}`, { method: 'POST', body })
    if (done) toast.add({ title: done, color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = false
  }
}
const approve = (e: SupportEvent) => act(`drafts/${e.id}/approve`, { text: edits[e.id] ?? e.body }, t('sup.sent'))
const reject = (e: SupportEvent) => act(`drafts/${e.id}/reject`)
const answer = ref('')
async function send() {
  if (!answer.value.trim()) return
  await act(`tickets/${props.ticketId}/reply`, { text: answer.value }, t('sup.sent'))
  answer.value = ''
}
async function setStatus(s: TicketStatus) {
  if (busy.value) return
  busy.value = true
  try {
    await $fetch(`${base.value}/tickets/${props.ticketId}`, { method: 'PATCH', body: { status: s } })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = false
  }
}
const who = (e: SupportEvent) => e.kind === 'customer_msg' ? (e.actor.replace(/^customer:/, '') || t('sup.customer')) : e.actor.replace(/^human:/, '')
</script>

<template>
  <div class="flex min-h-0 flex-col rounded-lg border border-(--ui-border)">
    <div class="flex flex-wrap items-center gap-2 border-b border-(--ui-border) p-2">
      <UButton class="md:hidden" size="xs" color="neutral" variant="ghost" icon="i-lucide-arrow-left" :aria-label="t('sup.back')" @click="emit('back')" />
      <p class="min-w-0 flex-1 truncate text-sm font-medium">{{ shown?.ticket.subject }}</p>
      <UBadge v-if="shown?.ticket.case_type" color="neutral" variant="outline" :label="shown.ticket.case_type" />
      <UBadge v-if="shown?.ticket.sla_breached" color="error" variant="subtle" icon="i-lucide-alarm-clock-off" :label="t('sup.slaBreached')" />
      <UBadge v-if="shown" :color="ticketColor[shown.ticket.status]" variant="subtle" :label="t(`sup.st.${shown.ticket.status}` as 'sup.st.new')" />
      <UButton v-if="isAdmin && shown && !['resolved', 'closed'].includes(shown.ticket.status)" size="xs" color="neutral" variant="outline" icon="i-lucide-check" :label="t('sup.resolve')" :loading="busy" @click="setStatus('resolved')" />
      <UButton v-else-if="isAdmin && shown" size="xs" color="neutral" variant="outline" icon="i-lucide-rotate-ccw" :label="t('sup.reopen')" :loading="busy" @click="setStatus('open')" />
    </div>
    <LoadingRows v-if="!shown" :n="4" />
    <template v-else>
      <div v-if="shown.customer" class="flex flex-wrap gap-x-3 gap-y-1 border-b border-(--ui-border) px-3 py-2 text-xs text-(--ui-text-muted)">
        <span class="font-medium text-(--ui-text)">{{ fieldText(shown.customer.data.name) }}</span>
        <span v-if="shown.customer.data.email">{{ shown.customer.data.email }}</span>
        <span v-if="shown.customer.data.phone">{{ shown.customer.data.phone }}</span>
        <span v-if="Array.isArray(shown.customer.data.tags) && shown.customer.data.tags.length">{{ fieldText(shown.customer.data.tags) }}</span>
      </div>
      <div class="min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
        <div v-for="e in lines" :key="e.id" class="flex" :class="e.kind === 'customer_msg' ? 'justify-start' : e.kind === 'status' ? 'justify-center' : 'justify-end'">
          <!-- what happened: handed to a person, SLA passed -->
          <p v-if="e.kind === 'status'" class="flex items-center gap-1.5 text-xs text-(--ui-text-muted)">
            <UIcon :name="e.body === 'sla_breached' ? 'i-lucide-alarm-clock-off' : 'i-lucide-user-round'" class="size-3.5" />{{ statusText(e) }} · {{ when(e.created_at) }}
          </p>
          <!-- an internal note: the customer never sees it -->
          <div v-else-if="e.kind === 'note'" class="max-w-xl rounded-lg border border-dashed border-(--ui-border) px-3 py-2">
            <p class="whitespace-pre-wrap text-sm text-(--ui-text-toned)">{{ e.body }}</p>
            <p class="mt-1 text-[11px] text-(--ui-text-dimmed)">{{ t('sup.note') }} · {{ who(e) }} · {{ when(e.created_at) }}</p>
          </div>
          <!-- an AI answer waiting for a person -->
          <div v-else-if="e.kind === 'ai_draft' && e.state === 'pending'" class="w-full max-w-xl space-y-2 rounded-lg border border-(--ui-warning)/40 bg-(--ui-warning)/5 p-3">
            <p class="flex items-center gap-1.5 text-xs font-medium text-(--ui-warning)"><UIcon name="i-lucide-sparkles" class="size-3.5" />{{ t('sup.draft') }}</p>
            <UTextarea v-if="isAdmin" :model-value="edits[e.id] ?? e.body" autoresize :rows="3" class="w-full" @update:model-value="v => edits[e.id] = String(v)" />
            <p v-else class="whitespace-pre-wrap text-sm">{{ e.body }}</p>
            <div v-if="isAdmin" class="flex justify-end gap-2">
              <UButton size="xs" color="neutral" variant="ghost" :label="t('sup.reject')" :disabled="busy" @click="reject(e)" />
              <UButton size="xs" icon="i-lucide-send" :label="t('sup.approve')" :loading="busy" @click="approve(e)" />
            </div>
          </div>
          <div v-else-if="e.kind === 'ai_draft'" class="max-w-xl rounded-lg border border-(--ui-error)/40 px-3 py-2 text-sm text-(--ui-error)">
            <UIcon name="i-lucide-triangle-alert" class="me-1 size-3.5 align-[-2px]" />{{ t('sup.failed') }}: {{ e.body }}
          </div>
          <div v-else class="max-w-xl rounded-lg px-3 py-2" :class="e.kind === 'customer_msg' ? 'bg-(--ui-bg-elevated)' : 'bg-primary/10'">
            <p class="whitespace-pre-wrap text-sm">{{ e.body }}</p>
            <p class="mt-1 text-[11px] text-(--ui-text-dimmed)">
              {{ who(e) }} · {{ when(e.created_at) }}<span v-if="e.meta?.edited"> · {{ t('sup.edited') }}</span>
              <span v-if="e.state === 'queued'"> · {{ t('sup.sending') }}</span>
              <span v-else-if="e.state === 'failed'" class="text-(--ui-error)"> · {{ t('sup.sendFailed') }}: {{ e.meta?.error }}</span>
            </p>
          </div>
        </div>
      </div>
      <form v-if="isAdmin" class="flex items-end gap-2 border-t border-(--ui-border) p-2" @submit.prevent="send">
        <UTextarea v-model="answer" :placeholder="t('sup.replyPlaceholder')" autoresize :rows="1" :maxrows="6" class="flex-1" />
        <UButton type="submit" icon="i-lucide-send" :aria-label="t('sup.send')" :loading="busy" :disabled="!answer.trim()" />
      </form>
    </template>
  </div>
</template>
