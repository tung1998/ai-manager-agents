<script setup lang="ts">
// An operation an agent proposed (a process/container operation or git commit/branch/push).
// Nothing happens until an admin approves.
export interface ProposedAction {
  id: string
  kind: string
  label: string
  target: string
  reason: string
  status: 'pending' | 'done' | 'failed' | 'rejected'
  detail: string
  proposed_by: string
  decided_by: string
  message?: string
  files?: string[]
}

const props = defineProps<{ action: ProposedAction, projectId?: string }>()
const emit = defineEmits<{ updated: [ProposedAction] }>()
const toast = useToast()
const { isAdmin } = useAuth()
const busy = ref<'' | 'approve' | 'reject'>('')

const icon = computed(() => props.action.kind === 'git_commit' ? 'i-lucide-git-commit-horizontal'
  : props.action.kind === 'git_branch' ? 'i-lucide-git-branch'
    : props.action.kind === 'git_push' ? 'i-lucide-upload'
      : props.action.kind.startsWith('stop') ? 'i-lucide-square'
  : props.action.kind.startsWith('restart') ? 'i-lucide-rotate-cw' : 'i-lucide-play')
const statusMeta: Record<ProposedAction['status'], { label: string, color: 'warning' | 'success' | 'neutral' | 'error' }> = {
  pending: { label: 'Chờ duyệt', color: 'warning' },
  done: { label: 'Đã thực hiện', color: 'success' },
  failed: { label: 'Lỗi', color: 'error' },
  rejected: { label: 'Đã từ chối', color: 'neutral' }
}
const opsLink = computed(() => props.projectId && !props.action.kind.startsWith('git_')
  ? `/projects/${props.projectId}?tab=ops&section=${props.action.kind.endsWith('container') ? 'containers' : 'processes'}`
  : '')

async function decide(approve: boolean) {
  busy.value = approve ? 'approve' : 'reject'
  try {
    const res = await $fetch<{ action: ProposedAction }>(`/api/actions/${props.action.id}/${approve ? 'approve' : 'reject'}`, { method: 'POST' })
    emit('updated', res.action)
    if (res.action.status === 'failed') toast.add({ title: 'Không thực hiện được', description: res.action.detail, color: 'error' })
  } catch (e) {
    const a = (e as { data?: { action?: ProposedAction } }).data?.action
    if (a) emit('updated', a)
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-3 rounded-lg border border-(--ui-border) px-3 py-2.5">
    <span class="grid size-8 shrink-0 place-items-center rounded-md bg-(--ui-bg-elevated)">
      <UIcon :name="icon" class="size-4 text-primary" />
    </span>
    <div class="min-w-0 flex-1">
      <p class="text-sm font-medium">{{ action.label }} <code>{{ action.target }}</code></p>
      <pre v-if="action.message" class="mt-1 whitespace-pre-wrap rounded bg-(--ui-bg-elevated) px-2 py-1 font-mono text-xs">{{ action.message }}</pre>
      <p v-if="action.files?.length" class="mt-1 truncate font-mono text-xs text-(--ui-text-muted)" :title="action.files.join('\n')">{{ action.files.length }} file: {{ action.files.join(', ') }}</p>
      <p v-if="action.reason" class="text-xs text-(--ui-text-muted)">{{ action.reason }}</p>
      <p v-if="action.status !== 'pending' && action.detail" class="text-xs" :class="action.status === 'failed' ? 'text-(--ui-error)' : 'text-(--ui-text-muted)'">{{ action.detail }}</p>
    </div>
    <UBadge :color="statusMeta[action.status].color" variant="subtle" size="sm" :label="statusMeta[action.status].label" />
    <UBadge v-if="action.decided_by?.startsWith('auto:')" color="warning" variant="outline" size="sm" icon="i-lucide-zap" label="tự động" :title="action.decided_by.slice(5)" />
    <template v-if="action.status === 'pending' && isAdmin">
      <UButton size="xs" icon="i-lucide-check" label="Duyệt" :loading="busy === 'approve'" :disabled="!!busy" @click="decide(true)" />
      <UButton size="xs" color="neutral" variant="ghost" label="Từ chối" :loading="busy === 'reject'" :disabled="!!busy" @click="decide(false)" />
    </template>
    <UButton v-else-if="action.status === 'done' && opsLink" size="xs" color="neutral" variant="ghost" icon="i-lucide-terminal" label="Xem log" :to="opsLink" />
  </div>
</template>
