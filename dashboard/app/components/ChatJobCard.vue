<script setup lang="ts">
// A job started in a chat (ADR-055): where it is, in the chat. What the
// person writes while it runs steers it; its result comes as the lead's answer.
export interface ChatJob {
  id: string, title: string, status: 'running' | 'done' | 'failed' | 'cancelled' | 'rejected' | 'needs_input',
  cost_usd: number, steps: number, current?: string, pending_patches?: number, applied_patches?: number, created_at: string
}
const props = defineProps<{ job: ChatJob, projectId: string }>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useLang()
const toast = useToast()

const meta = computed(() => {
  const j = props.job
  if (j.status === 'done' && (j.pending_patches ?? 0) > 0) return { label: t('status.awaitingApproval', { n: j.pending_patches ?? 0 }), color: 'warning' as const }
  return ({
    running: { label: t('status.running'), color: 'info' as const },
    done: { label: t('status.done'), color: 'success' as const },
    failed: { label: t('status.failed'), color: 'error' as const },
    cancelled: { label: t('status.cancelled'), color: 'neutral' as const },
    rejected: { label: t('status.rejected'), color: 'warning' as const },
    needs_input: { label: t('status.needsInput'), color: 'warning' as const }
  })[j.status]
})

const stopping = ref(false)
async function stop() {
  stopping.value = true
  try {
    await $fetch(`/api/tasks/${props.job.id}/cancel`, { method: 'POST' })
    emit('changed')
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    stopping.value = false
  }
}
</script>

<template>
  <div class="flex min-w-0 items-start gap-3 rounded-lg border border-(--ui-border) bg-(--ui-bg-elevated)/40 px-3 py-2.5 text-sm">
    <UIcon :name="job.status === 'running' ? 'i-lucide-loader-circle' : 'i-lucide-workflow'" class="mt-0.5 size-4 shrink-0 text-primary" :class="job.status === 'running' && 'animate-spin'" />
    <div class="min-w-0 flex-1">
      <p class="truncate font-medium">{{ t('job.chatTitle', { title: job.title }) }}</p>
      <p class="truncate text-xs text-(--ui-text-muted)">
        {{ t('job.chatSteps', { n: job.steps }) }}<template v-if="job.current"> · {{ job.current }}</template>
        <template v-if="job.cost_usd"> · ${{ job.cost_usd.toFixed(3) }}</template>
      </p>
      <p v-if="job.status === 'running'" class="mt-1 text-xs text-(--ui-text-muted)">{{ t('job.chatSteer') }}</p>
    </div>
    <div class="flex shrink-0 flex-wrap items-center justify-end gap-1">
      <UBadge :label="meta.label" :color="meta.color" variant="subtle" size="sm" />
      <UButton v-if="job.status === 'running'" size="xs" color="neutral" variant="ghost" icon="i-lucide-square" :loading="stopping" :aria-label="t('chat.stop')" @click="stop" />
      <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-external-link" :aria-label="t('job.chatOpen')" :to="{ path: `/projects/${projectId}`, query: { tab: 'tasks', task: job.id } }" />
    </div>
  </div>
</template>
