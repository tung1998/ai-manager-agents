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
  status: 'running' | 'done' | 'failed' | 'cancelled' | 'rejected' | 'needs_input'
  result: string
  detail: string
  budget_usd: number
  cost_usd: number
  attachments?: Attachment[]
  pending_patches?: number
  applied_patches?: number
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
const mode = ref<PermLevel>('propose')
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
    const d = await $fetch<Detail>(`/api/projects/${props.projectId}/tasks`, { method: 'POST', body: { goal: goal.value, budget_usd: Number(budget.value) || 0, attachments: goalFiles.value.map(a => a.id), mode: mode.value } })
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

// ---- follow-up talk with the lead, and committing the task's changes ----
const talkOpen = ref(false)
watch(() => detail.value?.task.id, () => { talkOpen.value = false })
async function reloadDetail() {
  if (!detail.value) return
  const d = await $fetch<Detail>(`/api/tasks/${detail.value.task.id}`).catch(() => null)
  if (d) detail.value = d
}

const commit = reactive({ open: false, loading: false, busy: false, message: '', files: [] as string[], picked: [] as string[] })
async function openCommit() {
  if (!detail.value) return
  Object.assign(commit, { open: true, loading: true, message: '', files: [], picked: [] })
  try {
    const d = await $fetch<{ message: string, files: string[] }>(`/api/tasks/${detail.value.task.id}/commit-draft`, { method: 'POST' })
    Object.assign(commit, { message: d.message, files: d.files, picked: [...d.files] })
  } catch (e) {
    commit.open = false
    toast.add({ title: apiError(e), color: 'warning' })
  } finally {
    commit.loading = false
  }
}
async function doCommit() {
  if (!detail.value) return
  commit.busy = true
  try {
    const res = await $fetch<{ action: ProposedAction }>(`/api/projects/${props.projectId}/git/commit`, { method: 'POST', body: { message: commit.message, files: commit.picked, task_id: detail.value.task.id } })
    commit.open = false
    toast.add({
      title: 'Đã commit', description: res.action.detail, color: 'success',
      actions: [{ label: 'Push', icon: 'i-lucide-upload', onClick: () => { push() } }]
    })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    commit.busy = false
  }
}
async function push() {
  try {
    const res = await $fetch<{ action: ProposedAction }>(`/api/projects/${props.projectId}/git/push`, { method: 'POST', body: {} })
    toast.add({ title: 'Đã push', description: res.action.detail, color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

async function cancel() {
  if (detail.value) await $fetch(`/api/tasks/${detail.value.task.id}/cancel`, { method: 'POST', body: {} }).catch(() => {})
}

// run a finished task again; learn = with last run's conclusion and failures
const retrying = ref<'' | 'plain' | 'learn'>('')
async function retry(t: Task, learn: boolean) {
  retrying.value = learn ? 'learn' : 'plain'
  try {
    const d = await $fetch<Detail>(`/api/tasks/${t.id}/retry`, { method: 'POST', body: { learn } })
    await refreshList()
    await open(d.task.id)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    retrying.value = ''
  }
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
  failed: { label: 'Không thành công', color: 'error' },
  cancelled: { label: 'Đã dừng', color: 'neutral' },
  rejected: { label: 'Không thông qua', color: 'warning' },
  needs_input: { label: 'Chờ bạn trả lời', color: 'warning' }
}
// a finished task whose diffs still wait for approval is not "done" for the person
function badge(t: Task) {
  if (t.status === 'done' && (t.pending_patches ?? 0) > 0) return { label: `Chờ duyệt (${t.pending_patches})`, color: 'warning' as const }
  return statusMeta[t.status]
}

// ---- approve / revert every diff of the task as one batch ----
const batchBusy = ref<'' | 'approve' | 'revert'>('')
const justApplied = ref(0)
const { data: procData } = useFetch<{ processes: { id: string, name: string, kind: string, command: string }[] }>(() => `/api/projects/${props.projectId}/processes`, { lazy: true })
const checkJobs = computed(() => (procData.value?.processes ?? []).filter(p => p.kind === 'job'))
const pendingPatches = computed(() => detail.value?.patches.filter(p => p.status === 'pending') ?? [])
const appliedPatches = computed(() => detail.value?.patches.filter(p => p.status === 'applied') ?? [])
const livePatches = computed(() => detail.value?.patches.filter(p => !(p.status === 'rejected' && p.detail.startsWith('Thay bằng bản sửa'))) ?? [])
const replacedPatches = computed(() => detail.value?.patches.filter(p => p.status === 'rejected' && p.detail.startsWith('Thay bằng bản sửa')) ?? [])
async function batch(kind: 'approve' | 'revert') {
  if (!detail.value) return
  if (kind === 'revert' && !confirm(`Hoàn tác ${appliedPatches.value.length} diff đã áp của Việc này?`)) return
  batchBusy.value = kind
  try {
    const res = await $fetch<{ patches: Patch[] }>(`/api/tasks/${detail.value.task.id}/patches/${kind === 'approve' ? 'approve-all' : 'revert-all'}`, { method: 'POST' })
    for (const p of res.patches) upsertPatch(p)
    justApplied.value = kind === 'approve' ? res.patches.length : 0
    toast.add({ title: kind === 'approve' ? `Đã áp ${res.patches.length} diff cùng lô` : `Đã hoàn tác ${res.patches.length} diff`, color: 'success' })
    await refreshList()
    await open(detail.value.task.id)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    batchBusy.value = ''
  }
}
async function runCheck(id: string, name: string) {
  try {
    await $fetch(`/api/processes/${id}/start`, { method: 'POST' })
    toast.add({ title: `Đang chạy ${name}`, description: 'Xem kết quả ở Vận hành → Tiến trình', color: 'info',
      actions: [{ label: 'Xem log', onClick: () => { navigateTo(`/projects/${props.projectId}?tab=ops`) } }] })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

const assignments = (s: Step) => (s.data.assignments as { agent: string, task: string, files?: string[], depends_on?: number[] }[] | undefined) ?? []
const conventions = (s: Step) => (s.data.conventions as string | undefined) ?? ''
const fixes = (s: Step) => (s.data.fixes as { job: number, issue: string }[] | undefined) ?? []
const verdictMeta: Record<string, { label: string, color: 'success' | 'warning' | 'error' | 'info' }> = {
  pass: { label: 'Đạt', color: 'success' },
  fix: { label: 'Cần sửa', color: 'warning' },
  ask: { label: 'Cần hỏi bạn', color: 'info' },
  fail: { label: 'Không sửa được', color: 'error' }
}

// a task that stopped with a question: answering starts it again with the reply
const answer = ref('')
async function reply(t: Task) {
  if (!answer.value.trim()) return
  retrying.value = 'learn'
  try {
    const d = await $fetch<Detail>(`/api/tasks/${t.id}/retry`, { method: 'POST', body: { learn: true, answer: answer.value } })
    answer.value = ''
    await refreshList()
    await open(d.task.id)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    retrying.value = ''
  }
}
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
            <UBadge :label="badge(t).label" :color="badge(t).color" variant="subtle" size="sm" />
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
          <div class="flex items-center gap-2 text-sm">
            <span class="text-(--ui-text-muted)">Chế độ</span>
            <ModePicker v-model="mode" :project-id="projectId" />
          </div>
        </div>
        <UButton icon="i-lucide-play" label="Bắt đầu" :loading="starting" :disabled="!goal.trim() || goalBox?.busy" @click="start" />
        <p class="text-xs text-(--ui-text-muted)">{{ permRank(mode) >= 3 ? 'Diff của agent có gói Tự sửa code sẽ được tự áp khi Việc hoàn tất và không bị phủ quyết.' : 'Thay đổi code chỉ được áp khi bạn duyệt.' }}</p>
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
            <!-- diffs waiting: the action itself replaces the "Chờ duyệt" label, right at the top -->
            <UButton
              v-if="isAdmin && detail.task.status === 'done' && pendingPatches.length" size="sm" icon="i-lucide-check-check"
              :label="`Duyệt tất cả (${pendingPatches.length})`" :loading="batchBusy === 'approve'" :disabled="!!batchBusy"
              title="Áp cả lô một lần: diff nào hỏng thì không áp gì" @click="batch('approve')"
            />
            <template v-else>
              <UBadge :label="badge(detail.task).label" :color="badge(detail.task).color" variant="subtle" />
              <UButton
                v-if="isAdmin && appliedPatches.length && !pendingPatches.length && detail.task.status !== 'running'" size="sm" color="neutral" variant="ghost"
                icon="i-lucide-undo-2" label="Hoàn tác cả lô" :loading="batchBusy === 'revert'" :disabled="!!batchBusy" @click="batch('revert')"
              />
            </template>
            <UButton
              v-if="isAdmin && appliedPatches.length && !pendingPatches.length && detail.task.status !== 'running'" size="sm" color="neutral" variant="outline"
              icon="i-lucide-git-commit-horizontal" label="Commit" title="Commit thay đổi của Việc này, AI gợi ý commit message" @click="openCommit"
            />
            <UButton v-if="detail.task.status === 'running'" icon="i-lucide-square" label="Dừng" size="sm" color="neutral" variant="outline" @click="cancel" />
            <template v-else>
              <UButton
                icon="i-lucide-graduation-cap" label="Chạy lại, rút kinh nghiệm" size="sm"
                :color="pendingPatches.length ? 'neutral' : 'primary'" :variant="pendingPatches.length || detail.task.status === 'done' ? 'outline' : 'solid'"
                :loading="retrying === 'learn'" :disabled="!!retrying" title="Chạy lại kèm kết luận và lỗi của lần này để đội tránh lặp lại"
                @click="retry(detail.task, true)"
              />
              <UButton
                icon="i-lucide-rotate-cw" label="Chạy lại" size="sm" color="neutral" variant="outline"
                :loading="retrying === 'plain'" :disabled="!!retrying" @click="retry(detail.task, false)"
              />
            </template>
          </div>
        </div>

        <details v-if="detail.task.goal.length > detail.task.title.length || detail.task.attachments?.length" class="rounded-lg border border-(--ui-border) p-3" :open="!!detail.task.attachments?.length">
          <summary class="cursor-pointer text-sm font-medium">Yêu cầu<span v-if="detail.task.attachments?.length" class="font-normal text-(--ui-text-muted)"> · {{ detail.task.attachments.length }} file</span></summary>
          <p class="mt-2 whitespace-pre-wrap text-sm">{{ detail.task.goal }}</p>
          <AttachmentList class="mt-2" :items="detail.task.attachments ?? []" />
        </details>

        <div v-if="detail.task.status === 'needs_input'" class="space-y-2 rounded-lg border border-(--ui-warning)/50 bg-(--ui-warning)/5 p-3">
          <p class="flex items-center gap-2 text-sm font-medium"><UIcon name="i-lucide-message-circle-question" class="size-4 text-(--ui-warning)" /> Đội cần bạn quyết định</p>
          <p class="text-sm">{{ detail.task.detail }}</p>
          <UTextarea v-model="answer" :rows="2" autoresize class="w-full" placeholder="Trả lời của bạn…" />
          <UButton icon="i-lucide-send" label="Trả lời và chạy tiếp" size="sm" :loading="retrying === 'learn'" :disabled="!answer.trim()" @click="reply(detail.task)" />
        </div>

        <UAlert v-if="detail.task.detail && detail.task.status !== 'done' && detail.task.status !== 'needs_input'" :color="detail.task.status === 'rejected' ? 'warning' : 'error'" variant="subtle" :description="detail.task.detail" />

        <UCard v-if="detail.task.result">
          <template #header><p class="font-medium">Kết quả</p></template>
          <!-- eslint-disable-next-line vue/no-v-html -->
          <div class="markdown text-sm" v-html="renderMarkdown(detail.task.result)" />
        </UCard>

        <div v-if="detail.task.status !== 'running'" class="space-y-2">
          <UButton
            v-if="!talkOpen" icon="i-lucide-messages-square" label="Trao đổi với quản lý" size="sm" color="neutral" variant="outline"
            @click="talkOpen = true"
          />
          <template v-else>
            <p class="text-sm font-medium">Trao đổi với quản lý</p>
            <ChatPanel :key="detail.task.id" :project-id="projectId" :task-id="detail.task.id" @turn-done="reloadDetail" />
          </template>
        </div>

        <div v-if="detail.actions?.length" class="space-y-2">
          <p class="text-sm font-medium">Đề xuất thao tác</p>
          <ActionCard
            v-for="a in detail.actions ?? []" :key="a.id" :action="a" :project-id="projectId"
            @updated="(na: ProposedAction) => { if (detail) detail.actions = (detail.actions ?? []).map(x => x.id === na.id ? na : x) }"
          />
        </div>
        <div v-if="detail.patches.length" class="space-y-2">
          <div class="flex flex-wrap items-center gap-2">
            <p class="text-sm font-medium">Đề xuất thay đổi code</p>
            <span class="text-xs text-(--ui-text-muted)">{{ pendingPatches.length }} chờ duyệt · {{ appliedPatches.length }} đã áp</span>
            <div v-if="isAdmin && detail.task.status !== 'running'" class="ms-auto flex gap-2">
              <UButton
                v-if="pendingPatches.length" size="sm" icon="i-lucide-check-check" :label="`Duyệt tất cả (${pendingPatches.length})`"
                :loading="batchBusy === 'approve'" :disabled="!!batchBusy" title="Áp cả lô một lần: diff nào hỏng thì không áp gì"
                @click="batch('approve')"
              />
              <UButton
                v-if="appliedPatches.length" size="sm" color="neutral" variant="outline" icon="i-lucide-undo-2" label="Hoàn tác cả lô"
                :loading="batchBusy === 'revert'" :disabled="!!batchBusy" @click="batch('revert')"
              />
            </div>
          </div>
          <div v-if="justApplied && checkJobs.length" class="flex flex-wrap items-center gap-2 rounded-md border border-(--ui-success)/40 bg-(--ui-success)/5 px-3 py-2 text-sm">
            <UIcon name="i-lucide-circle-check" class="size-4 text-(--ui-success)" />
            Đã áp {{ justApplied }} diff. Chạy kiểm tra:
            <UButton v-for="j in checkJobs" :key="j.id" size="xs" color="neutral" variant="outline" icon="i-lucide-play" :label="j.name" @click="runCheck(j.id, j.name)" />
          </div>
          <p v-else-if="justApplied" class="text-xs text-(--ui-text-muted)">Đã áp {{ justApplied }} diff. Nên thêm lệnh typecheck/test ở Vận hành → Tiến trình để kiểm tra.</p>
          <PatchCard v-for="p in livePatches" :key="p.id" :patch="p" @updated="upsertPatch" />
          <details v-if="replacedPatches.length" class="text-sm">
            <summary class="cursor-pointer text-xs text-(--ui-text-muted)">Bản cũ đã thay bằng bản sửa ({{ replacedPatches.length }})</summary>
            <div class="mt-2 space-y-2 opacity-70">
              <PatchCard v-for="p in replacedPatches" :key="p.id" :patch="p" @updated="upsertPatch" />
            </div>
          </details>
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
                  :color="verdictMeta[String(s.data.verdict)]?.color ?? 'warning'" :label="verdictMeta[String(s.data.verdict)]?.label ?? 'Chưa đạt'"
                />
                <span v-if="s.phase === 'review' && Number(s.data.round) > 0" class="text-xs text-(--ui-text-muted)">sau vòng sửa {{ s.data.round }}</span>
                <UBadge v-if="s.status === 'failed'" size="sm" color="error" variant="subtle" label="Lỗi" />
                <span class="ms-auto text-xs text-(--ui-text-muted)">
                  <template v-if="s.tools.length || liveTools[s.id]?.length">{{ (s.tools.length || liveTools[s.id]?.length) }} công cụ · </template>
                  <template v-if="s.cost_usd">${{ s.cost_usd.toFixed(3) }}</template>
                </span>
              </summary>
              <div class="space-y-2 border-t border-(--ui-border) px-3 py-2 text-sm">
                <p v-if="s.phase === 'work'" class="text-xs text-(--ui-text-muted)">Việc được giao: {{ s.instruction }}</p>
                <p v-if="s.phase === 'vote' && s.data.reason" class="text-xs">{{ s.data.reason }}</p>
                <ul v-if="fixes(s).length" class="space-y-1 rounded-md bg-(--ui-bg-elevated) px-2 py-1.5 text-xs">
                  <li v-for="(f, i) in fixes(s)" :key="i"><span class="font-medium">Việc {{ f.job }} cần sửa:</span> {{ f.issue }}</li>
                </ul>
                <p v-if="conventions(s)" class="rounded-md bg-(--ui-bg-elevated) px-2 py-1.5 text-xs">
                  <span class="font-medium">Quy ước chung:</span> {{ conventions(s) }}
                </p>
                <ul v-if="assignments(s).length" class="space-y-1.5">
                  <li v-for="(a, i) in assignments(s)" :key="i" class="text-xs">
                    <span class="me-1 text-(--ui-text-dimmed)">{{ i + 1 }}.</span>
                    <UBadge :label="a.agent" size="sm" color="neutral" variant="outline" class="me-1 font-mono" />{{ a.task }}
                    <span v-if="a.files?.length || a.depends_on?.length" class="mt-0.5 block ps-4 text-(--ui-text-muted)">
                      <template v-if="a.files?.length">sửa: <code>{{ a.files.join(', ') }}</code></template>
                      <template v-if="a.depends_on?.length"> · sau việc {{ a.depends_on.join(', ') }}</template>
                    </span>
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
    <UModal v-model:open="commit.open" title="Commit thay đổi của Việc">
      <template #body>
        <div class="space-y-3">
          <div v-if="commit.loading" class="flex items-center gap-2 text-sm text-(--ui-text-muted)">
            <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" /> Đang soạn commit message…
          </div>
          <template v-else>
            <UTextarea v-model="commit.message" :rows="4" autoresize class="w-full font-mono text-xs" />
            <div class="space-y-1">
              <p class="text-xs text-(--ui-text-muted)">File ({{ commit.picked.length }}/{{ commit.files.length }})</p>
              <label v-for="f in commit.files" :key="f" class="flex cursor-pointer items-center gap-2 text-xs">
                <UCheckbox :model-value="commit.picked.includes(f)" @update:model-value="(v: boolean | 'indeterminate') => commit.picked = v === true ? [...commit.picked, f] : commit.picked.filter(x => x !== f)" />
                <code class="truncate">{{ f }}</code>
              </label>
            </div>
          </template>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="commit.open = false" />
          <UButton icon="i-lucide-git-commit-horizontal" label="Commit" :loading="commit.busy" :disabled="commit.loading || !commit.message.trim() || !commit.picked.length" @click="doCommit" />
        </div>
      </template>
    </UModal>
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
