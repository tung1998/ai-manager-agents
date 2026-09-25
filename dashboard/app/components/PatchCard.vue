<script setup lang="ts">
export interface Patch {
  id: string
  diff: string
  files: string[]
  status: 'pending' | 'applied' | 'rejected' | 'failed'
  detail: string
  decided_by: string
}

const props = defineProps<{ patch: Patch }>()
const emit = defineEmits<{ updated: [Patch] }>()
const toast = useToast()
const { isAdmin } = useAuth()
const { t } = useLang()
const busy = ref<'' | 'approve' | 'reject'>('')
const open = ref(props.patch.status === 'pending' || props.patch.status === 'failed')

const lines = computed(() => props.patch.diff.split('\n').map((l) => {
  const kind = l.startsWith('+++') || l.startsWith('---') ? 'file'
    : l.startsWith('@@') ? 'hunk'
      : l.startsWith('+') ? 'add'
        : l.startsWith('-') ? 'del' : 'ctx'
  return { text: l, kind }
}))
const stats = computed(() => ({
  add: lines.value.filter(l => l.kind === 'add').length,
  del: lines.value.filter(l => l.kind === 'del').length
}))

const statusMeta = computed<Record<Patch['status'], { label: string, color: 'warning' | 'success' | 'neutral' | 'error', icon: string }>>(() => ({
  pending: { label: t('patch.pending'), color: 'warning', icon: 'i-lucide-clock' },
  applied: { label: t('patch.applied'), color: 'success', icon: 'i-lucide-circle-check' },
  rejected: { label: t('patch.rejected'), color: 'neutral', icon: 'i-lucide-circle-x' },
  failed: { label: t('patch.failed'), color: 'error', icon: 'i-lucide-triangle-alert' }
}))

async function decide(approve: boolean) {
  busy.value = approve ? 'approve' : 'reject'
  try {
    const res = await $fetch<{ patch: Patch }>(`/api/patches/${props.patch.id}/${approve ? 'approve' : 'reject'}`, { method: 'POST', body: {} })
    emit('updated', res.patch)
    if (res.patch.status === 'applied') toast.add({ title: t('patch.appliedToast'), description: res.patch.files.join(', '), color: 'success' })
    if (res.patch.status === 'failed') toast.add({ title: t('patch.failedToast'), description: res.patch.detail, color: 'error' })
    open.value = res.patch.status === 'failed'
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="overflow-hidden rounded-lg border border-(--ui-border)">
    <div class="flex flex-wrap items-center gap-2 bg-(--ui-bg-muted) px-3 py-2 text-sm">
      <button type="button" class="flex min-w-0 flex-1 items-center gap-2 text-left" @click="open = !open">
        <UIcon :name="open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-4 shrink-0" />
        <UIcon name="i-lucide-file-diff" class="size-4 shrink-0 text-primary" />
        <span class="truncate font-mono text-xs">{{ patch.files.join(', ') }}</span>
        <span class="shrink-0 text-xs"><span class="text-(--ui-success)">+{{ stats.add }}</span> <span class="text-(--ui-error)">−{{ stats.del }}</span></span>
      </button>
      <UBadge :label="statusMeta[patch.status].label" :color="statusMeta[patch.status].color" :icon="statusMeta[patch.status].icon" variant="subtle" size="sm" />
      <UBadge v-if="patch.decided_by?.startsWith('auto:')" color="warning" variant="outline" size="sm" icon="i-lucide-zap" :label="t('patch.auto')" :title="patch.decided_by.slice(5)" />
      <template v-if="patch.status === 'pending' && isAdmin">
        <UButton size="xs" color="neutral" variant="ghost" :label="t('patch.reject')" :loading="busy === 'reject'" :disabled="!!busy" @click="decide(false)" />
        <UButton size="xs" icon="i-lucide-check" :label="t('patch.approveApply')" :loading="busy === 'approve'" :disabled="!!busy" @click="decide(true)" />
      </template>
    </div>
    <p v-if="patch.detail && patch.status !== 'applied'" class="border-t border-(--ui-border) px-3 py-1.5 text-xs text-(--ui-error)">{{ patch.detail }}</p>
    <pre v-if="open" class="max-h-96 overflow-auto border-t border-(--ui-border) py-1 text-xs leading-5"><code><span
      v-for="(l, i) in lines" :key="i" class="block px-3"
      :class="{
        'bg-(--ui-success)/10 text-(--ui-success)': l.kind === 'add',
        'bg-(--ui-error)/10 text-(--ui-error)': l.kind === 'del',
        'text-(--ui-text-muted)': l.kind === 'hunk' || l.kind === 'file'
      }"
    >{{ l.text || ' ' }}</span></code></pre>
  </div>
</template>
