<script setup lang="ts">
import type { Attachment } from './PromptInput.vue'
import type { Patch } from './PatchCard.vue'
import type { ProposedAction } from './ActionCard.vue'

interface ToolCall { name: string, summary: string, error?: boolean }
interface Task {
  id: string
  title: string
  goal: string
  mode: 'single' | 'hierarchy' | 'council'
  status: 'running' | 'done' | 'failed' | 'cancelled' | 'rejected'
  result: string
  detail: string
  budget_usd: number
  cost_usd: number
  attachments?: Attachment[]
  created_by: string
  created_at: string
}
interface Step {
  id: string
  seq: number
  phase: 'plan' | 'vote' | 'revise' | 'work' | 'review' | 'synthesize'
  agent_key: string
  agent_name: string
  instruction: string
  output: string
  data: Record<string, unknown>
  tools: ToolCall[]
  status: 'running' | 'done' | 'failed' | 'skipped'
  error: string
  cost_usd: number | null
}
interface Detail { task: Task, steps: Step[], patches: Patch[], actions?: ProposedAction[], running: boolean }
interface TaskEvent { type: string, step_id?: string, step?: Step, text?: string, tool?: ToolCall, patch?: Patch, task?: Task }

const props = defineProps<{ projectId: string, modelKind: string, governance: string }>()
const toast = useToast()
const { isAdmin } = useAuth()

const { data: listData, refresh: refreshList } = await useFetch<{ tasks: Task[] }>(() => `/api/projects/${props.projectId}/tasks`)
const tasks = computed(() => listData.value?.tasks ?? [])

const detail = ref<Detail | null>(null)
const liveText = reactive<Record<string, string>>({})
const liveTools = reactive<Record<string, ToolCall[]>>({})
const statusLine = ref('')
const showNew = ref(false)
const goal = ref('')
const goalFiles = ref<Attachment[]>([])
const goalBox = ref<{ busy: boolean } | null>(null)
const budget = ref(1)
const starting = ref(false)
let source: EventSource | null = null

const flowHint = computed(() => ({
  single: 'Agent lead làm trực tiếp.',
  hierarchy: 'Trưởng nhóm lập kế hoạch và giao việc → đội làm song song → trưởng nhóm tổng hợp.',
  council: 'Lập kế hoạch đề xuất → hội đồng biểu quyết (2/3, Giám sát có quyền phủ quyết) → worker làm → Giám sát kiểm tra → Thực thi tổng hợp.'
} as Record<string, string>)[props.governance] ?? '')

async function open(id: string) {
  source?.close()
  showNew.value = false
  statusLine.value = ''
  for (const k of Object.keys(liveText)) delete liveText[k]
  for (const k of Object.keys(liveTools)) delete liveTools[k]
  detail.value = await $fetch<Detail>(`/api/tasks/${id}`)
  if (detail.value.running) follow(id)
}

function upsertStep(s: Step) {
  if (!detail.value) return
  const i = detail.value.steps.findIndex(x => x.id === s.id)
  if (i >= 0) detail.value.steps[i] = s
  else detail.value.steps.push(s)
}
function upsertPatch(p: Patch) {
  if (!detail.value) return
  const i = detail.value.patches.findIndex(x => x.id === p.id)
  if (i >= 0) detail.value.patches[i] = p
  else detail.value.patches.push(p)
}

function follow(id: string) {
  source?.close()
  source = new EventSource(`/api/tasks/${id}/stream`)
  source.onmessage = (m) => {
    const ev = JSON.parse(m.data) as TaskEvent
    switch (ev.type) {
      case 'status': statusLine.value = ev.text ?? ''; break
      case 'step': if (ev.step) upsertStep(ev.step); break
      case 'text': if (ev.step_id) liveText[ev.step_id] = (liveText[ev.step_id] ?? '') + (ev.text ?? ''); break
      case 'tool': if (ev.step_id && ev.tool) (liveTools[ev.step_id] ??= []).push(ev.tool); break
      case 'step_done': if (ev.step) upsertStep(ev.step); break
      case 'patch': if (ev.patch) upsertPatch(ev.patch); break
      case 'done':
        source?.close()
        source = null
        refreshList()
        open(id)
        break
    }
  }
  source.onerror = () => {
    if (source?.readyState === EventSource.CLOSED) {
      source = null
      open(id)
    }
  }
}

async function start() {
  starting.value = true
  try {
    const d = await $fetch<Detail>(`/api/projects/${props.projectId}/tasks`, { method: 'POST', body: { goal: goal.value, budget_usd: Number(budget.value) || 0, attachments: goalFiles.value.map(a => a.id) } })
    goal.value = ''
    goalFiles.value = []
    await refreshList()
    await open(d.task.id)
  } catch (e) {
    const d = (e as { data?: { code?: string, error?: string } }).data
    toast.add({ title: d?.code === 'budget' ? 'Đã chạm trần chi phí ngày' : apiError(e), description: d?.code === 'budget' ? d.error : undefined, color: 'error' })
  } finally {
    starting.value = false
  }
}

async function cancel() {
  if (detail.value) await $fetch(`/api/tasks/${detail.value.task.id}/cancel`, { method: 'POST', body: {} }).catch(() => {})
}

async function remove(t: Task) {
  if (!confirm('Xóa việc này và lịch sử của nó?')) return
  try {
    await $fetch(`/api/tasks/${t.id}`, { method: 'DELETE' })
    if (detail.value?.task.id === t.id) detail.value = null
    refreshList()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

const phaseLabel: Record<Step['phase'], string> = {
  plan: 'Lập kế hoạch', revise: 'Sửa kế hoạch', vote: 'Biểu quyết', work: 'Làm việc', review: 'Kiểm tra', synthesize: 'Tổng hợp'
}
const phaseIcon: Record<Step['phase'], string> = {
  plan: 'i-lucide-list-checks', revise: 'i-lucide-list-restart', vote: 'i-lucide-vote', work: 'i-lucide-wrench', review: 'i-lucide-shield-check', synthesize: 'i-lucide-sparkles'
}
const statusMeta: Record<Task['status'], { label: string, color: 'info' | 'success' | 'error' | 'neutral' | 'warning' }> = {
  running: { label: 'Đang chạy', color: 'info' },
  done: { label: 'Xong', color: 'success' },
  failed: { label: 'Lỗi', color: 'error' },
  cancelled: { label: 'Đã dừng', color: 'neutral' },
  rejected: { label: 'Không thông qua', color: 'warning' }
}
const assignments = (s: Step) => (s.data.assignments as { agent: string, task: string }[] | undefined) ?? []
const when = (d: string) => new Date(d).toLocaleString('vi-VN', { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })

onMounted(() => {
  const wanted = useRoute().query.task as string | undefined
  if (wanted) open(wanted)
  else if (tasks.value[0]) open(tasks.value[0].id)
  else showNew.value = true
})
onBeforeUnmount(() => source?.close())
</script>

<template>
  <div class="flex h-[calc(100vh-13rem)] min-h-[28rem] overflow-hidden rounded-lg border border-(--ui-border)">
    <aside class="hidden w-64 shrink-0 flex-col border-e border-(--ui-border) md:flex">
      <div class="border-b border-(--ui-border) p-2">
        <UButton icon="i-lucide-plus" label="Giao việc mới" size="sm" color="neutral" variant="ghost" block class="justify-start" @click="showNew = true; detail = null" />
      </div>
      <div class="flex-1 overflow-y-auto p-1">
        <p v-if="!tasks.length" class="p-3 text-xs text-(--ui-text-muted)">Chưa có việc nào.</p>
        <div
          v-for="t in tasks" :key="t.id"
          class="group cursor-pointer rounded-md px-2 py-1.5 text-sm"
          :class="detail?.task.id === t.id ? 'bg-(--ui-bg-accented)' : 'hover:bg-(--ui-bg-muted)'"
          @click="open(t.id)"
        >
          <div class="flex items-start gap-1">
            <p class="min-w-0 flex-1 truncate">{{ t.title }}</p>
            <button v-if="isAdmin && t.status !== 'running'" type="button" class="invisible text-(--ui-text-dimmed) group-hover:visible" @click.stop="remove(t)">
              <UIcon name="i-lucide-trash-2" class="size-3.5" />
            </button>
          </div>
          <div class="mt-0.5 flex items-center gap-1.5 text-xs text-(--ui-text-muted)">
            <UBadge :label="statusMeta[t.status].label" :color="statusMeta[t.status].color" variant="subtle" size="sm" />
            <span>{{ when(t.created_at) }}</span>
            <span v-if="t.cost_usd">· ${{ t.cost_usd.toFixed(3) }}</span>
          </div>
        </div>
      </div>
    </aside>

    <section class="min-w-0 flex-1 overflow-y-auto p-4">
      <!-- new task -->
      <div v-if="showNew || !detail" class="mx-auto max-w-2xl space-y-4">
        <div>
          <p class="text-lg font-semibold">Giao việc cho cả mô hình</p>
          <p class="text-sm text-(--ui-text-muted)">{{ flowHint }}</p>
        </div>
        <PromptInput
          ref="goalBox" v-model="goal" v-model:attachments="goalFiles" :project-id="projectId" :rows="5" :maxrows="16" :submit-on-enter="false"
          placeholder="VD: Tìm nguyên nhân lỗi checkout khi chọn thanh toán PayPal và đề xuất cách sửa. Đính kèm ảnh lỗi, log hoặc tài liệu liên quan."
        />
        <div class="flex flex-wrap items-center gap-3">
          <UFormField label="Trần chi phí cho việc này (USD)" class="w-56">
            <UInputNumber v-model="budget" :min="0" :step="0.5" size="sm" />
          </UFormField>
          <p class="text-xs text-(--ui-text-muted)">0 là không giới hạn (vẫn áp trần theo ngày).</p>
        </div>
        <UButton icon="i-lucide-play" label="Bắt đầu" :loading="starting" :disabled="!goal.trim() || goalBox?.busy" @click="start" />
        <p class="text-xs text-(--ui-text-muted)">Các agent chỉ đọc project. Thay đổi code chỉ được áp khi bạn duyệt.</p>
      </div>

      <!-- task detail -->
      <div v-else class="mx-auto max-w-3xl space-y-4">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <p class="text-lg font-semibold">{{ detail.task.title }}</p>
            <p class="text-xs text-(--ui-text-muted)">
              {{ detail.task.created_by.replace('human:', '') }} · {{ when(detail.task.created_at) }}
              · ${{ detail.task.cost_usd.toFixed(3) }}<template v-if="detail.task.budget_usd"> / ${{ detail.task.budget_usd }}</template>
            </p>
          </div>
          <div class="flex items-center gap-2">
            <UBadge :label="statusMeta[detail.task.status].label" :color="statusMeta[detail.task.status].color" variant="subtle" />
            <UButton v-if="detail.task.status === 'running'" icon="i-lucide-square" label="Dừng" size="sm" color="neutral" variant="outline" @click="cancel" />
          </div>
        </div>

        <details v-if="detail.task.goal.length > detail.task.title.length || detail.task.attachments?.length" class="rounded-lg border border-(--ui-border) p-3" :open="!!detail.task.attachments?.length">
          <summary class="cursor-pointer text-sm font-medium">Yêu cầu<span v-if="detail.task.attachments?.length" class="font-normal text-(--ui-text-muted)"> · {{ detail.task.attachments.length }} file</span></summary>
          <p class="mt-2 whitespace-pre-wrap text-sm">{{ detail.task.goal }}</p>
          <AttachmentList class="mt-2" :items="detail.task.attachments ?? []" />
        </details>

        <UAlert v-if="detail.task.detail && detail.task.status !== 'done'" :color="detail.task.status === 'rejected' ? 'warning' : 'error'" variant="subtle" :description="detail.task.detail" />

        <UCard v-if="detail.task.result">
          <template #header><p class="font-medium">Kết quả</p></template>
          <!-- eslint-disable-next-line vue/no-v-html -->
          <div class="markdown text-sm" v-html="renderMarkdown(detail.task.result)" />
        </UCard>

        <div v-if="detail.actions?.length" class="space-y-2">
          <p class="text-sm font-medium">Đề xuất thao tác</p>
          <ActionCard
            v-for="a in detail.actions ?? []" :key="a.id" :action="a" :project-id="projectId"
            @updated="(na: ProposedAction) => { if (detail) detail.actions = (detail.actions ?? []).map(x => x.id === na.id ? na : x) }"
          />
        </div>
        <div v-if="detail.patches.length" class="space-y-2">
          <p class="text-sm font-medium">Đề xuất thay đổi code</p>
          <PatchCard v-for="p in detail.patches" :key="p.id" :patch="p" @updated="upsertPatch" />
        </div>

        <div v-if="detail.task.status === 'running' && statusLine" class="flex items-center gap-2 text-sm text-(--ui-text-muted)">
          <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin text-primary" />{{ statusLine }}
        </div>

        <!-- timeline -->
        <ol class="relative space-y-3 border-s border-(--ui-border) ps-5">
          <li v-for="s in detail.steps" :key="s.id" class="relative">
            <span class="absolute -start-[1.72rem] top-1 flex size-6 items-center justify-center rounded-full border border-(--ui-border) bg-(--ui-bg)">
              <UIcon v-if="s.status === 'running'" name="i-lucide-loader-circle" class="size-3.5 animate-spin text-primary" />
              <UIcon v-else :name="phaseIcon[s.phase]" class="size-3.5" :class="s.status === 'failed' ? 'text-(--ui-error)' : 'text-primary'" />
            </span>
            <details :open="s.status === 'running' || s.phase === 'plan'" class="rounded-lg border border-(--ui-border)">
              <summary class="flex cursor-pointer flex-wrap items-center gap-2 px-3 py-2 text-sm">
                <span class="font-medium">{{ phaseLabel[s.phase] }}</span>
                <span class="text-(--ui-text-muted)">· {{ s.agent_name }}</span>
                <UBadge
                  v-if="s.phase === 'vote' && s.data.vote" size="sm" variant="subtle"
                  :color="s.data.vote === 'approve' ? 'success' : 'error'" :label="s.data.vote === 'approve' ? 'Đồng ý' : 'Phản đối'"
                />
                <UBadge
                  v-if="s.phase === 'review' && s.data.verdict" size="sm" variant="subtle"
                  :color="s.data.verdict === 'pass' ? 'success' : 'warning'" :label="s.data.verdict === 'pass' ? 'Đạt' : 'Chưa đạt'"
                />
                <UBadge v-if="s.status === 'failed'" size="sm" color="error" variant="subtle" label="Lỗi" />
                <span class="ms-auto text-xs text-(--ui-text-muted)">
                  <template v-if="s.tools.length || liveTools[s.id]?.length">{{ (s.tools.length || liveTools[s.id]?.length) }} công cụ · </template>
                  <template v-if="s.cost_usd">${{ s.cost_usd.toFixed(3) }}</template>
                </span>
              </summary>
              <div class="space-y-2 border-t border-(--ui-border) px-3 py-2 text-sm">
                <p v-if="s.phase === 'work'" class="text-xs text-(--ui-text-muted)">Việc được giao: {{ s.instruction }}</p>
                <p v-if="s.phase === 'vote' && s.data.reason" class="text-xs">{{ s.data.reason }}</p>
                <ul v-if="assignments(s).length" class="space-y-1">
                  <li v-for="(a, i) in assignments(s)" :key="i" class="text-xs">
                    <UBadge :label="a.agent" size="sm" color="neutral" variant="outline" class="me-1 font-mono" />{{ a.task }}
                  </li>
                </ul>
                <p v-if="s.error" class="text-xs text-(--ui-error)">{{ s.error }}</p>
                <!-- eslint-disable-next-line vue/no-v-html -->
                <div v-if="s.output || liveText[s.id]" class="markdown text-sm" v-html="renderMarkdown(s.output || liveText[s.id] || '')" />
                <details v-if="(s.tools.length ? s.tools : liveTools[s.id] ?? []).length" class="text-xs text-(--ui-text-muted)">
                  <summary class="cursor-pointer">Công cụ đã dùng</summary>
                  <ul class="mt-1 ps-4">
                    <li v-for="(t, i) in (s.tools.length ? s.tools : liveTools[s.id] ?? [])" :key="i">{{ t.summary }}</li>
                  </ul>
                </details>
              </div>
            </details>
          </li>
        </ol>
      </div>
    </section>
  </div>
</template>

<style scoped>
.markdown :deep(p) { margin: 0.4rem 0; }
.markdown :deep(ul) { list-style: disc; padding-inline-start: 1.25rem; margin: 0.4rem 0; }
.markdown :deep(ol) { list-style: decimal; padding-inline-start: 1.25rem; margin: 0.4rem 0; }
.markdown :deep(code) { font-family: ui-monospace, monospace; font-size: 0.85em; background: var(--ui-bg-muted); padding: 0.1rem 0.3rem; border-radius: 0.25rem; }
.markdown :deep(pre) { background: var(--ui-bg-muted); padding: 0.75rem; border-radius: 0.5rem; overflow-x: auto; }
.markdown :deep(pre code) { background: none; padding: 0; }
.markdown :deep(h1), .markdown :deep(h2), .markdown :deep(h3) { font-weight: 600; margin: 0.75rem 0 0.25rem; }
</style>
