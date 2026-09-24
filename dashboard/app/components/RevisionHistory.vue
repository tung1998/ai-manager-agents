<script setup lang="ts">
interface Revision {
  id: string
  action: string
  actor: string
  agent_count: number
  created_at: string
}
interface Snapshot {
  name: string
  kind: string
  agents: { key: string, name: string, tier: AgentTier, llm_model?: string, model_tier: ModelTier }[]
}

const props = defineProps<{ modelId: string }>()
const emit = defineEmits<{ restored: [] }>()
const open = defineModel<boolean>('open', { default: false })

const toast = useToast()
const { isAdmin } = useAuth()
const revisions = ref<Revision[]>([])
const loading = ref(false)
const expanded = ref<Record<string, Snapshot | null>>({})

async function load() {
  loading.value = true
  try {
    revisions.value = (await $fetch<{ revisions: Revision[] }>(`/api/org-models/${props.modelId}/revisions`)).revisions
  } finally {
    loading.value = false
  }
}
watch(open, (v) => { if (v) load() })

async function toggle(r: Revision) {
  if (expanded.value[r.id] !== undefined) {
    const { [r.id]: _, ...rest } = expanded.value
    expanded.value = rest
    return
  }
  expanded.value = { ...expanded.value, [r.id]: null }
  const res = await $fetch<{ snapshot: Snapshot }>(`/api/revisions/${r.id}`)
  expanded.value = { ...expanded.value, [r.id]: res.snapshot }
}

async function restore(r: Revision) {
  if (!confirm('Đưa mô hình về trạng thái này? Trạng thái hiện tại vẫn được lưu lại trong lịch sử.')) return
  try {
    await $fetch(`/api/revisions/${r.id}/restore`, { method: 'POST', body: {} })
    toast.add({ title: 'Đã khôi phục', color: 'success' })
    emit('restored')
    await load()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

// A revision is the state *before* the action it names.
function describe(action: string) {
  const [kind, arg] = action.split(':')
  const map: Record<string, string> = {
    'model.update': 'sửa cài đặt mô hình',
    'agent.update': `sửa agent ${arg}`,
    'agent.create': `thêm agent ${arg}`,
    'agent.delete': `xóa agent ${arg}`,
    'model.replace': 'đổi sang mô hình khác',
    'template.reset': 'khôi phục mặc định',
    'restore': 'khôi phục một phiên bản cũ',
    'import': 'nhập config'
  }
  return `Trước khi ${map[kind!] ?? action}`
}
const who = (a: string) => a.replace(/^human:/, '')
</script>

<template>
  <USlideover v-model:open="open" title="Lịch sử chỉnh sửa" :ui="{ content: 'max-w-lg' }">
    <template #body>
      <p class="mb-4 text-sm text-(--ui-text-muted)">
        Mỗi lần sửa, office lưu lại trạng thái ngay trước đó (tối đa 50 bản). Khôi phục một bản cũng được lưu, nên luôn hoàn tác được.
      </p>
      <p v-if="loading" class="text-sm text-(--ui-text-muted)">Đang tải…</p>
      <p v-else-if="!revisions.length" class="text-sm text-(--ui-text-muted)">Chưa có chỉnh sửa nào.</p>
      <ol class="space-y-2">
        <li v-for="r in revisions" :key="r.id" class="rounded-lg border border-(--ui-border) p-3">
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <p class="text-sm font-medium">{{ describe(r.action) }}</p>
              <p class="text-xs text-(--ui-text-muted)">
                {{ new Date(r.created_at).toLocaleString('vi-VN') }} · {{ who(r.actor) }} · {{ r.agent_count }} agent
              </p>
            </div>
            <div class="flex shrink-0 gap-1">
              <UButton size="xs" color="neutral" variant="ghost" :label="expanded[r.id] !== undefined ? 'Ẩn' : 'Xem'" @click="toggle(r)" />
              <UButton v-if="isAdmin" size="xs" variant="soft" icon="i-lucide-rotate-ccw" label="Khôi phục" @click="restore(r)" />
            </div>
          </div>
          <div v-if="expanded[r.id] !== undefined" class="mt-2 rounded bg-(--ui-bg-muted) p-2 text-xs">
            <p v-if="!expanded[r.id]">Đang tải…</p>
            <template v-else>
              <p class="font-medium">{{ expanded[r.id]!.name }}</p>
              <ul class="mt-1 space-y-0.5">
                <li v-for="a in expanded[r.id]!.agents" :key="a.key">
                  <span class="text-(--ui-text-muted)">{{ tierLabel[a.tier] }}</span> · {{ a.name }}
                  <code class="text-(--ui-text-muted)">{{ a.key }}</code>
                </li>
              </ul>
            </template>
          </div>
        </li>
      </ol>
    </template>
  </USlideover>
</template>
