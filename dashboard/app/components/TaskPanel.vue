<script setup lang="ts">
import type { Attachment } from './PromptInput.vue'
import type { Patch } from './PatchCard.vue'
import type { ProposedAction } from './ActionCard.vue'

interface ToolCall { name: string, summary: string, error?: boolean }
interface Task {
  source?: Source
  id: string
  title: string
  goal: string
  mode: 'single' | 'hierarchy' | 'council'
  status: 'running' | 'done' | 'failed' | 'cancelled' | 'rejected' | 'needs_input'
  result: string
  detail: string
  budget_usd: number
  cost_usd: number
  assignee_id?: string
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
  started_at?: string
  agent_id?: string
}
interface Detail { task: Task, steps: Step[], patches: Patch[], actions?: ProposedAction[], running: boolean }
interface TaskEvent { type: string, step_id?: string, step?: Step, text?: string, tool?: ToolCall, patch?: Patch, task?: Task }

const props = defineProps<{ projectId: string, modelKind: string, governance: string }>()
const toast = useToast()
const { isAdmin } = useAuth()
const { t, dateLocale } = useLang()

// where the tasks came from: the dashboard, a bot (/job), an automation
const origin = ref<'all' | Source>('all')
const { data: listData, refresh: refreshList } = await useFetch<{ tasks: Task[] }>(() => `/api/projects/${props.projectId}/tasks?source=${origin.value}`)
const tasks = computed(() => listData.value?.tasks ?? [])

const detail = ref<Detail | null>(null)
const listOpen = ref(false) // the list over the page on a phone
const resultOpen = ref(false) // a task's result read in a big window
const liveText = reactive<Record<string, string>>({})
const liveTools = reactive<Record<string, ToolCall[]>>({})
const statusLine = ref('')
const showNew = ref(false)
const goal = ref('')
const goalFiles = ref<Attachment[]>([])
const goalBox = ref<{ busy: boolean } | null>(null)
const budget = ref(0) // 0 = no cap for this task (the office's daily cap still applies)
const advanced = ref(false)
// no separate mode: each agent's own rights decide (members are capped server-side)
const mode = 'operate'
const editMode = ref<'worktree' | 'direct'>('worktree')
// who does it: the team (the lead splits it) or one agent alone
const TEAM = '__team'
const assignee = ref(TEAM)
const { data: agentsData } = useFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
// the diffs apply on their own when the one agent may (the team: a person approves)
const autoApplies = computed(() => {
  const a = agentsData.value?.agents.find(x => x.id === assignee.value)
  return !!a && agentCaps(a.permissions).includes('code.apply')
})
const agentName = (id?: string) => agentsData.value?.agents.find(a => a.id === id)?.name ?? ''
const assigneeItems = computed(() => [{ label: t('task.assignTeam'), value: TEAM }, ...(agentsData.value?.agents ?? []).map(a => ({ label: a.name, value: a.id }))])
const starting = ref(false)
let source: EventSource | null = null

const flowHint = computed(() => ({
  single: t('flow.single'),
  hierarchy: t('flow.hierarchy'),
  council: t('flow.council')
} as Record<string, string>)[props.governance] ?? '')

// the open task lives in the URL, so a link points at it
const route = useRoute()
const router = useRouter()
const copy = useCopy()
const moreLabel = computed(() => t('chat.more'))
const taskLink = (id: string) => `${location.origin}/projects/${props.projectId}?tab=tasks&task=${id}`
function taskMenu(task: Task) {
  const items: { label: string, icon: string, color?: 'error', onSelect: () => void }[][] = [[
    { label: t('task.copyLink'), icon: 'i-lucide-link', onSelect: () => copy(taskLink(task.id)) },
    { label: t('task.copyId'), icon: 'i-lucide-hash', onSelect: () => copy(task.id) }
  ]]
  if (isAdmin.value && task.status !== 'running') items.push([{ label: t('common.delete'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => remove(task) }])
  return items
}

async function open(id: string) {
  if (route.query.task !== id) router.replace({ query: { ...route.query, task: id } })
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
    const d = await $fetch<Detail & { queued?: boolean }>(`/api/projects/${props.projectId}/tasks`, { method: 'POST', body: { goal: goal.value, budget_usd: Number(budget.value) || 0, attachments: goalFiles.value.map(a => a.id), mode, edit_mode: editMode.value, agent_id: assignee.value === TEAM ? '' : assignee.value } })
    goal.value = ''
    goalFiles.value = []
    if (d.queued) {
      // the project runs another task: this one waits as a queued job
      toast.add({ title: t('job.queued'), color: 'info', actions: [{ label: t('nav.jobs'), to: `/jobs?project=${props.projectId}` }] })
      return
    }
    await refreshList()
    await open(d.task.id)
  } catch (e) {
    const d = (e as { data?: { code?: string, error?: string } }).data
    toast.add({ title: d?.code === 'budget' ? t('task.budgetHit') : apiError(e), description: d?.code === 'budget' ? d.error : undefined, color: 'error' })
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
      title: t('task.committed'), description: res.action.detail, color: 'success',
      actions: [{ label: t('task.push'), icon: 'i-lucide-upload', onClick: () => { push() } }]
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
    toast.add({ title: t('task.pushed'), description: res.action.detail, color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

async function cancel() {
  if (detail.value) await $fetch(`/api/tasks/${detail.value.task.id}/cancel`, { method: 'POST', body: {} }).catch(() => {})
}

// run a finished task again; learn = with last run's conclusion and failures
const retrying = ref<'' | 'plain' | 'learn'>('')
async function retry(task: Task, learn: boolean) {
  retrying.value = learn ? 'learn' : 'plain'
  try {
    const d = await $fetch<Detail>(`/api/tasks/${task.id}/retry`, { method: 'POST', body: { learn } })
    await refreshList()
    await open(d.task.id)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    retrying.value = ''
  }
}

async function remove(task: Task) {
  if (!confirm(t('task.deleteConfirm'))) return
  try {
    await $fetch(`/api/tasks/${task.id}`, { method: 'DELETE' })
    if (detail.value?.task.id === task.id) detail.value = null
    refreshList()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

// the task as a group conversation: who assigned what, who reported back
type TaskView = 'chat' | 'steps'
const view = ref<TaskView>('chat')
onMounted(() => {
  try {
    const v = localStorage.getItem('office-task-view')
    if (v === 'chat' || v === 'steps') view.value = v
  } catch { /* storage blocked: default view */ }
})
watch(view, (v) => {
  try { localStorage.setItem('office-task-view', v) } catch { /* ignore */ }
})
const expanded = ref<Record<string, boolean>>({})
// plan/review output is the agent's JSON: shown as its parts, not raw
const showsOutput = (s: Step) => !((s.phase === 'plan' || s.phase === 'revise') && assignments(s).length)

const phaseLabel = computed<Record<Step['phase'], string>>(() => ({
  plan: t('phase.plan'), revise: t('phase.revise'), vote: t('phase.vote'), work: t('phase.work'), review: t('phase.review'), synthesize: t('phase.synthesize')
}))
const phaseIcon: Record<Step['phase'], string> = {
  plan: 'i-lucide-list-checks', revise: 'i-lucide-list-restart', vote: 'i-lucide-vote', work: 'i-lucide-wrench', review: 'i-lucide-shield-check', synthesize: 'i-lucide-sparkles'
}
const statusMeta = computed<Record<Task['status'], { label: string, color: 'info' | 'success' | 'error' | 'neutral' | 'warning' }>>(() => ({
  running: { label: t('status.running'), color: 'info' },
  done: { label: t('status.done'), color: 'success' },
  failed: { label: t('status.failed'), color: 'error' },
  cancelled: { label: t('status.cancelled'), color: 'neutral' },
  rejected: { label: t('status.rejected'), color: 'warning' },
  needs_input: { label: t('status.needsInput'), color: 'warning' }
}))
// a finished task whose diffs still wait for approval is not "done" for the person
function badge(task: Task) {
  if (task.status === 'done' && (task.pending_patches ?? 0) > 0) return { label: t('status.awaitingApproval', { n: task.pending_patches ?? 0 }), color: 'warning' as const }
  return statusMeta.value[task.status]
}

// ---- approve / revert every diff of the task as one batch ----
const batchBusy = ref<'' | 'approve' | 'revert'>('')
const justApplied = ref(0)
const { data: procData } = useFetch<{ processes: { id: string, name: string, kind: string, command: string }[] }>(() => `/api/projects/${props.projectId}/processes`, { lazy: true })
const checkJobs = computed(() => (procData.value?.processes ?? []).filter(p => p.kind === 'job'))
const pendingPatches = computed(() => detail.value?.patches.filter(p => p.status === 'pending') ?? [])
const appliedPatches = computed(() => detail.value?.patches.filter(p => p.status === 'applied') ?? [])
const livePatches = computed(() => detail.value?.patches.filter(p => !(p.status === 'rejected' && p.detail.startsWith('Thay bằng bản sửa'))) ?? []) // i18n-ignore: matches backend-generated marker text
const replacedPatches = computed(() => detail.value?.patches.filter(p => p.status === 'rejected' && p.detail.startsWith('Thay bằng bản sửa')) ?? []) // i18n-ignore: matches backend-generated marker text
async function batch(kind: 'approve' | 'revert') {
  if (!detail.value) return
  if (kind === 'revert' && !confirm(t('task.revertAllConfirm', { n: appliedPatches.value.length }))) return
  batchBusy.value = kind
  try {
    const res = await $fetch<{ patches: Patch[] }>(`/api/tasks/${detail.value.task.id}/patches/${kind === 'approve' ? 'approve-all' : 'revert-all'}`, { method: 'POST' })
    for (const p of res.patches) upsertPatch(p)
    justApplied.value = kind === 'approve' ? res.patches.length : 0
    toast.add({ title: kind === 'approve' ? t('task.applied', { n: res.patches.length }) : t('task.reverted', { n: res.patches.length }), color: 'success' })
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
    toast.add({ title: t('task.appliedRunning', { name }), description: t('task.appliedSeeLog'), color: 'info',
      actions: [{ label: t('task.seeLog'), onClick: () => { navigateTo(`/projects/${props.projectId}?tab=ops`) } }] })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

const assignments = (s: Step) => (s.data.assignments as { agent: string, task: string, files?: string[], depends_on?: number[] }[] | undefined) ?? []
const conventions = (s: Step) => (s.data.conventions as string | undefined) ?? ''
const fixes = (s: Step) => (s.data.fixes as { job: number, issue: string }[] | undefined) ?? []
const verdictMeta = computed<Record<string, { label: string, color: 'success' | 'warning' | 'error' | 'info' }>>(() => ({
  pass: { label: t('verdict.pass'), color: 'success' },
  fix: { label: t('verdict.fix'), color: 'warning' },
  ask: { label: t('verdict.ask'), color: 'info' },
  fail: { label: t('verdict.fail'), color: 'error' }
}))

// a task that stopped with a question: answering starts it again with the reply
const answer = ref('')
async function reply(task: Task) {
  if (!answer.value.trim()) return
  retrying.value = 'learn'
  try {
    const d = await $fetch<Detail>(`/api/tasks/${task.id}/retry`, { method: 'POST', body: { learn: true, answer: answer.value } })
    answer.value = ''
    await refreshList()
    await open(d.task.id)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    retrying.value = ''
  }
}
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })

onMounted(() => {
  const wanted = useRoute().query.task as string | undefined
  if (wanted) open(wanted)
  else if (tasks.value[0]) open(tasks.value[0].id)
  else showNew.value = true
})
onBeforeUnmount(() => source?.close())
</script>

<template>
  <div class="flex min-h-[24rem] flex-1 overflow-hidden rounded-lg border border-(--ui-border)">
    <aside class="hidden w-64 shrink-0 flex-col border-e border-(--ui-border) md:flex">
      <TaskList v-model:origin="origin" :tasks="tasks" :current-id="detail?.task.id" :menu="taskMenu" :badge="badge" @open="open" @new="showNew = true; detail = null" />
    </aside>
    <!-- a phone: the tasks in a drawer, as the chats -->
    <USlideover v-model:open="listOpen" side="left" :title="t('task.list')" :ui="{ content: 'max-w-xs', body: 'p-0 sm:p-0 flex flex-col' }">
      <template #body>
        <TaskList
          v-model:origin="origin" :tasks="tasks" :current-id="detail?.task.id" :menu="taskMenu" :badge="badge"
          @open="(id) => { listOpen = false; open(id) }" @new="listOpen = false; showNew = true; detail = null"
        />
      </template>
    </USlideover>

    <section class="flex min-w-0 flex-1 flex-col">
      <!-- a phone: which task this is, the drawer of tasks, a new one (as the chats) -->
      <div class="flex items-center gap-1 border-b border-(--ui-border) p-2 md:hidden">
        <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-panel-left" :aria-label="t('task.list')" @click="listOpen = true" />
        <p class="min-w-0 flex-1 truncate text-sm font-medium">{{ (!showNew && detail?.task.title) || t('task.newTitle') }}</p>
        <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-plus" :aria-label="t('task.new')" @click="showNew = true; detail = null" />
      </div>
      <div class="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden p-4">
      <!-- new task -->
      <div v-if="showNew || !detail" class="mx-auto max-w-2xl space-y-4">
        <div>
          <p class="text-lg font-semibold">{{ t('task.newTitle') }}</p>
          <p class="text-sm text-(--ui-text-muted)">{{ flowHint }}</p>
        </div>
        <PromptInput
          ref="goalBox" v-model="goal" v-model:attachments="goalFiles" :project-id="projectId" :rows="5" :maxrows="16" :submit-on-enter="false"
          :placeholder="t('task.goalPlaceholder')"
        />
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex items-center gap-2 text-sm">
            <span class="text-(--ui-text-muted)">{{ t('task.assignTo') }}</span>
            <USelect v-model="assignee" :items="assigneeItems" size="sm" class="min-w-44" />
          </div>
          <div class="flex items-center gap-2 text-sm">
            <span class="text-(--ui-text-muted)">{{ t('task.modeLabel') }}</span>
            <EditModePicker v-model="editMode" />
          </div>
          <UButton
            size="xs" color="neutral" variant="ghost" :label="t('task.advanced')"
            :trailing-icon="advanced ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'" @click="advanced = !advanced"
          />
        </div>
        <div v-if="advanced" class="flex flex-wrap items-center gap-3 rounded-md border border-(--ui-border) p-3">
          <UFormField :label="t('task.budgetLabel')" class="w-56">
            <UInputNumber v-model="budget" :min="0" :step="0.5" size="sm" />
          </UFormField>
          <p class="text-xs text-(--ui-text-muted)">{{ t('task.budgetHint') }}</p>
        </div>
        <UButton icon="i-lucide-play" :label="t('task.start')" :loading="starting" :disabled="!goal.trim() || goalBox?.busy" @click="start" />
        <p class="text-xs text-(--ui-text-muted)">{{ autoApplies ? t('task.autoApplyHint') : t('task.manualApplyHint') }}</p>
      </div>

      <!-- task detail -->
      <div v-else class="mx-auto max-w-3xl space-y-4">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <p class="text-lg font-semibold">{{ detail.task.title }}</p>
            <p class="text-xs text-(--ui-text-muted)">
              {{ detail.task.created_by.replace('human:', '') }}<template v-if="detail.task.assignee_id"> → {{ agentName(detail.task.assignee_id) || t('task.oneAgent') }}</template> · {{ when(detail.task.created_at) }}
              · ${{ detail.task.cost_usd.toFixed(3) }}<template v-if="detail.task.budget_usd"> / ${{ detail.task.budget_usd }}</template>
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2"> <!-- the buttons wrap on a phone -->
            <!-- diffs waiting: the action itself replaces the pending-approval label, right at the top -->
            <UButton
              v-if="isAdmin && detail.task.status === 'done' && pendingPatches.length" size="sm" icon="i-lucide-check-check"
              :label="t('task.approveAll', { n: pendingPatches.length })" :loading="batchBusy === 'approve'" :disabled="!!batchBusy"
              :title="t('task.approveAllTitle')" @click="batch('approve')"
            />
            <template v-else>
              <UBadge :label="badge(detail.task).label" :color="badge(detail.task).color" variant="subtle" />
              <UButton
                v-if="isAdmin && appliedPatches.length && !pendingPatches.length && detail.task.status !== 'running'" size="sm" color="neutral" variant="ghost"
                icon="i-lucide-undo-2" :label="t('task.revertAll')" :loading="batchBusy === 'revert'" :disabled="!!batchBusy" @click="batch('revert')"
              />
            </template>
            <UButton
              v-if="isAdmin && appliedPatches.length && !pendingPatches.length && detail.task.status !== 'running'" size="sm" color="neutral" variant="outline"
              icon="i-lucide-git-commit-horizontal" :label="t('task.commit')" :title="t('task.commitTitle')" @click="openCommit"
            />
            <UButton v-if="detail.task.status === 'running'" icon="i-lucide-square" :label="t('task.stop')" size="sm" color="neutral" variant="outline" @click="cancel" />
            <template v-else>
              <UButton
                icon="i-lucide-graduation-cap" :label="t('task.retryLearn')" size="sm"
                :color="pendingPatches.length ? 'neutral' : 'primary'" :variant="pendingPatches.length || detail.task.status === 'done' ? 'outline' : 'solid'"
                :loading="retrying === 'learn'" :disabled="!!retrying" :title="t('task.retryLearnTitle')"
                @click="retry(detail.task, true)"
              />
              <UButton
                icon="i-lucide-rotate-cw" :label="t('task.retry')" size="sm" color="neutral" variant="outline"
                :loading="retrying === 'plain'" :disabled="!!retrying" @click="retry(detail.task, false)"
              />
            </template>
          </div>
        </div>

        <details v-if="detail.task.goal.length > detail.task.title.length || detail.task.attachments?.length" class="rounded-lg border border-(--ui-border) p-3" :open="!!detail.task.attachments?.length">
          <summary class="cursor-pointer text-sm font-medium">{{ t('task.requestSection') }}<span v-if="detail.task.attachments?.length" class="font-normal text-(--ui-text-muted)"> · {{ t('task.fileCount', { n: detail.task.attachments.length }) }}</span></summary>
          <p class="mt-2 whitespace-pre-wrap text-sm">{{ detail.task.goal }}</p>
          <AttachmentList class="mt-2" :items="detail.task.attachments ?? []" />
        </details>

        <div v-if="detail.task.status === 'needs_input'" class="space-y-2 rounded-lg border border-(--ui-warning)/50 bg-(--ui-warning)/5 p-3">
          <p class="flex items-center gap-2 text-sm font-medium"><UIcon name="i-lucide-message-circle-question" class="size-4 text-(--ui-warning)" /> {{ t('task.needsInputTitle') }}</p>
          <p class="text-sm">{{ detail.task.detail }}</p>
          <UTextarea v-model="answer" :rows="2" autoresize class="w-full" :placeholder="t('task.answerPlaceholder')" />
          <UButton icon="i-lucide-send" :label="t('task.answerSend')" size="sm" :loading="retrying === 'learn'" :disabled="!answer.trim()" @click="reply(detail.task)" />
        </div>

        <UAlert v-if="detail.task.detail && detail.task.status !== 'done' && detail.task.status !== 'needs_input'" :color="detail.task.status === 'rejected' ? 'warning' : 'error'" variant="subtle" :description="detail.task.detail" />

        <!-- the result: a preview; a click reads it all in a big window -->
        <UCard v-if="detail.task.result" :ui="{ header: 'py-2 sm:py-2.5' }" class="cursor-pointer transition hover:ring-1 hover:ring-(--ui-border-accented)" @click="resultOpen = true">
          <template #header>
            <div class="flex items-center gap-1">
              <p class="min-w-0 flex-1 font-medium">{{ t('task.result') }}</p>
              <UIcon name="i-lucide-maximize-2" class="size-4 text-(--ui-text-muted)" :title="t('task.readResult')" />
            </div>
          </template>
          <div class="relative max-h-48 overflow-hidden">
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div class="markdown text-sm" v-html="renderMarkdown(detail.task.result)" />
            <div class="absolute inset-x-0 bottom-0 flex h-16 items-end justify-center bg-linear-to-t from-(--ui-bg) to-transparent pb-1 text-xs text-primary">{{ t('task.readResult') }}</div>
          </div>
        </UCard>
        <UModal v-model:open="resultOpen" :title="t('task.result')" :description="detail.task.title" fullscreen :ui="{ description: 'line-clamp-1' }">
          <template #body>
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div class="markdown mx-auto max-w-3xl text-sm" v-html="renderMarkdown(detail.task.result)" />
          </template>
        </UModal>

        <div v-if="detail.task.status !== 'running'" class="space-y-2">
          <UButton
            v-if="!talkOpen" icon="i-lucide-messages-square" :label="t('task.talkWithManager')" size="sm" color="neutral" variant="outline"
            @click="talkOpen = true"
          />
          <template v-else>
            <p class="text-sm font-medium">{{ t('task.talkWithManager') }}</p>
            <ChatPanel :key="detail.task.id" :project-id="projectId" :task-id="detail.task.id" @turn-done="reloadDetail" />
          </template>
        </div>

        <div v-if="detail.actions?.length" class="space-y-2">
          <p class="text-sm font-medium">{{ t('task.proposedActions') }}</p>
          <ActionCard
            v-for="a in detail.actions ?? []" :key="a.id" :action="a" :project-id="projectId"
            @updated="(na: ProposedAction) => { if (detail) detail.actions = (detail.actions ?? []).map(x => x.id === na.id ? na : x) }"
          />
        </div>
        <div v-if="detail.patches.length" class="space-y-2">
          <div class="flex flex-wrap items-center gap-2">
            <p class="text-sm font-medium">{{ t('task.proposedChanges') }}</p>
            <span class="text-xs text-(--ui-text-muted)">{{ t('task.pendingApproved', { pending: pendingPatches.length, applied: appliedPatches.length }) }}</span>
            <div v-if="isAdmin && detail.task.status !== 'running'" class="ms-auto flex gap-2">
              <UButton
                v-if="pendingPatches.length" size="sm" icon="i-lucide-check-check" :label="t('task.approveAll', { n: pendingPatches.length })"
                :loading="batchBusy === 'approve'" :disabled="!!batchBusy" :title="t('task.approveAllTitle')"
                @click="batch('approve')"
              />
              <UButton
                v-if="appliedPatches.length" size="sm" color="neutral" variant="outline" icon="i-lucide-undo-2" :label="t('task.revertAll')"
                :loading="batchBusy === 'revert'" :disabled="!!batchBusy" @click="batch('revert')"
              />
            </div>
          </div>
          <div v-if="justApplied && checkJobs.length" class="flex flex-wrap items-center gap-2 rounded-md border border-(--ui-success)/40 bg-(--ui-success)/5 px-3 py-2 text-sm">
            <UIcon name="i-lucide-circle-check" class="size-4 text-(--ui-success)" />
            {{ t('task.appliedRunChecks', { n: justApplied }) }}
            <UButton v-for="j in checkJobs" :key="j.id" size="xs" color="neutral" variant="outline" icon="i-lucide-play" :label="j.name" @click="runCheck(j.id, j.name)" />
          </div>
          <p v-else-if="justApplied" class="text-xs text-(--ui-text-muted)">{{ t('task.appliedHint', { n: justApplied }) }}</p>
          <PatchCard v-for="p in livePatches" :key="p.id" :patch="p" @updated="upsertPatch" />
          <details v-if="replacedPatches.length" class="text-sm">
            <summary class="cursor-pointer text-xs text-(--ui-text-muted)">{{ t('task.replacedPatches', { n: replacedPatches.length }) }}</summary>
            <div class="mt-2 space-y-2 opacity-70">
              <PatchCard v-for="p in replacedPatches" :key="p.id" :patch="p" @updated="upsertPatch" />
            </div>
          </details>
        </div>

        <div v-if="detail.task.status === 'running' && statusLine" class="flex items-center gap-2 text-sm text-(--ui-text-muted)">
          <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin text-primary" />{{ statusLine }}
        </div>

        <div class="flex justify-end">
          <div class="flex rounded-lg bg-(--ui-bg-elevated) p-0.5 text-xs">
            <button
              v-for="v in (['chat', 'steps'] as const)" :key="v" type="button" class="flex items-center gap-1 rounded-md px-2.5 py-1"
              :class="view === v ? 'bg-(--ui-bg) font-medium shadow-sm' : 'text-(--ui-text-muted) hover:text-(--ui-text)'" @click="view = v"
            >
              <UIcon :name="v === 'chat' ? 'i-lucide-messages-square' : 'i-lucide-list-tree'" class="size-3.5" />{{ v === 'chat' ? t('task.viewChat') : t('task.viewSteps') }}
            </button>
          </div>
        </div>

        <!-- the team's conversation -->
        <div v-if="view === 'chat'" class="space-y-4">
          <div v-for="s in detail.steps" :key="s.id" class="flex gap-3">
            <AgentAvatar :agent="agentsData?.agents.find(a => a.id === s.agent_id || a.name === s.agent_name) ?? { name: s.agent_name }" size="md" />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
                <span class="text-sm font-medium">{{ s.agent_name }}</span>
                <span class="flex items-center gap-1 text-(--ui-text-muted)"><UIcon :name="phaseIcon[s.phase]" class="size-3" />{{ phaseLabel[s.phase] }}</span>
                <span v-if="s.started_at" class="text-(--ui-text-dimmed)">{{ when(s.started_at) }}</span>
                <span v-if="s.cost_usd" class="text-(--ui-text-dimmed)">· ${{ s.cost_usd.toFixed(3) }}</span>
              </div>
              <div class="break-anywhere mt-1 space-y-2 rounded-lg rounded-tl-none bg-(--ui-bg-elevated)/60 px-3 py-2 text-sm">
                <p v-if="s.phase === 'work' && s.instruction" class="border-s-2 border-(--ui-border-accented) ps-2 text-xs text-(--ui-text-muted)">{{ t('task.gotAssigned', { instruction: s.instruction }) }}</p>
                <template v-if="s.phase === 'vote' && s.data.vote">
                  <UBadge size="sm" variant="subtle" :color="s.data.vote === 'approve' ? 'success' : 'error'" :label="s.data.vote === 'approve' ? t('vote.approve') : t('vote.reject')" />
                  <p v-if="s.data.reason">{{ s.data.reason }}</p>
                </template>
                <UBadge
                  v-if="s.phase === 'review' && s.data.verdict" size="sm" variant="subtle"
                  :color="verdictMeta[String(s.data.verdict)]?.color ?? 'warning'" :label="verdictMeta[String(s.data.verdict)]?.label ?? t('verdict.fix')"
                />
                <ul v-if="fixes(s).length" class="space-y-1 text-xs">
                  <li v-for="(f, i) in fixes(s)" :key="i"><span class="font-medium">{{ t('task.jobNeedsFix', { n: f.job }) }}</span> {{ f.issue }}</li>
                </ul>
                <p v-if="conventions(s)" class="text-xs"><span class="font-medium">{{ t('task.conventions') }}</span> {{ conventions(s) }}</p>
                <ul v-if="assignments(s).length" class="space-y-1.5">
                  <li v-for="(a, i) in assignments(s)" :key="i">
                    <span class="me-1 font-medium text-primary">@{{ a.agent }}</span>{{ a.task }}
                    <span v-if="a.files?.length" class="block text-xs text-(--ui-text-muted)">{{ t('task.editsFiles') }} <code>{{ a.files.join(', ') }}</code></span>
                  </li>
                </ul>
                <div v-if="showsOutput(s) && (s.output || liveText[s.id])" class="relative" :class="!expanded[s.id] && s.status !== 'running' ? 'max-h-60 overflow-hidden' : ''">
                  <!-- eslint-disable-next-line vue/no-v-html -->
                  <div class="markdown text-sm" v-html="renderMarkdown(s.output || liveText[s.id] || '')" />
                  <div v-if="!expanded[s.id] && s.status !== 'running' && (s.output || '').length > 900" class="absolute inset-x-0 bottom-0 flex h-16 items-end justify-center bg-gradient-to-t from-(--ui-bg-elevated) to-transparent">
                    <UButton size="xs" color="neutral" variant="soft" :label="t('task.showMore')" @click="expanded[s.id] = true" />
                  </div>
                </div>
                <p v-if="s.status === 'running' && !liveText[s.id]" class="flex items-center gap-1.5 text-xs text-(--ui-text-muted)">
                  <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin" />{{ t('task.working') }}
                  <template v-if="liveTools[s.id]?.length"> · {{ liveTools[s.id]!.at(-1)!.summary }}</template>
                </p>
                <p v-if="s.error" class="text-xs text-(--ui-error)">{{ s.error }}</p>
                <p v-if="s.status !== 'running' && s.tools.length" class="text-xs text-(--ui-text-dimmed)">{{ t('task.toolsUsed', { n: s.tools.length }) }}</p>
              </div>
            </div>
          </div>
        </div>

        <!-- timeline -->
        <ol v-else class="relative space-y-3 border-s border-(--ui-border) ps-5">
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
                  :color="s.data.vote === 'approve' ? 'success' : 'error'" :label="s.data.vote === 'approve' ? t('vote.approve') : t('vote.reject')"
                />
                <UBadge
                  v-if="s.phase === 'review' && s.data.verdict" size="sm" variant="subtle"
                  :color="verdictMeta[String(s.data.verdict)]?.color ?? 'warning'" :label="verdictMeta[String(s.data.verdict)]?.label ?? t('verdict.fix')"
                />
                <span v-if="s.phase === 'review' && Number(s.data.round) > 0" class="text-xs text-(--ui-text-muted)">{{ t('task.afterRound', { n: Number(s.data.round) }) }}</span>
                <UBadge v-if="s.status === 'failed'" size="sm" color="error" variant="subtle" :label="t('step.error')" />
                <span class="ms-auto text-xs text-(--ui-text-muted)">
                  <template v-if="s.tools.length || liveTools[s.id]?.length">{{ t('task.toolsUsed', { n: (s.tools.length || liveTools[s.id]?.length || 0) }) }} · </template>
                  <template v-if="s.cost_usd">${{ s.cost_usd.toFixed(3) }}</template>
                </span>
              </summary>
              <div class="space-y-2 border-t border-(--ui-border) px-3 py-2 text-sm">
                <p v-if="s.phase === 'work'" class="text-xs text-(--ui-text-muted)">{{ t('task.workAssigned', { instruction: s.instruction }) }}</p>
                <p v-if="s.phase === 'vote' && s.data.reason" class="text-xs">{{ s.data.reason }}</p>
                <ul v-if="fixes(s).length" class="space-y-1 rounded-md bg-(--ui-bg-elevated) px-2 py-1.5 text-xs">
                  <li v-for="(f, i) in fixes(s)" :key="i"><span class="font-medium">{{ t('task.jobNeedsFix', { n: f.job }) }}</span> {{ f.issue }}</li>
                </ul>
                <p v-if="conventions(s)" class="rounded-md bg-(--ui-bg-elevated) px-2 py-1.5 text-xs">
                  <span class="font-medium">{{ t('task.conventions') }}</span> {{ conventions(s) }}
                </p>
                <ul v-if="assignments(s).length" class="space-y-1.5">
                  <li v-for="(a, i) in assignments(s)" :key="i" class="text-xs">
                    <span class="me-1 text-(--ui-text-dimmed)">{{ i + 1 }}.</span>
                    <UBadge :label="a.agent" size="sm" color="neutral" variant="outline" class="me-1 font-mono" />{{ a.task }}
                    <span v-if="a.files?.length || a.depends_on?.length" class="mt-0.5 block ps-4 text-(--ui-text-muted)">
                      <template v-if="a.files?.length">{{ t('task.editsFiles') }} <code>{{ a.files.join(', ') }}</code></template>
                      <template v-if="a.depends_on?.length"> · {{ t('task.afterJobs', { jobs: a.depends_on.join(', ') }) }}</template>
                    </span>
                  </li>
                </ul>
                <p v-if="s.error" class="text-xs text-(--ui-error)">{{ s.error }}</p>
                <!-- eslint-disable-next-line vue/no-v-html -->
                <div v-if="s.output || liveText[s.id]" class="markdown text-sm" v-html="renderMarkdown(s.output || liveText[s.id] || '')" />
                <details v-if="(s.tools.length ? s.tools : liveTools[s.id] ?? []).length" class="text-xs text-(--ui-text-muted)">
                  <summary class="cursor-pointer">{{ t('task.toolsUsedDetail') }}</summary>
                  <ul class="mt-1 ps-4">
                    <li v-for="(t2, i) in (s.tools.length ? s.tools : liveTools[s.id] ?? [])" :key="i">{{ t2.summary }}</li>
                  </ul>
                </details>
              </div>
            </details>
          </li>
        </ol>
      </div>
      </div>
    </section>
    <UModal v-model:open="commit.open" :title="t('task.commitTitle2')">
      <template #body>
        <div class="space-y-3">
          <div v-if="commit.loading" class="flex items-center gap-2 text-sm text-(--ui-text-muted)">
            <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" /> {{ t('task.commitComposing') }}
          </div>
          <template v-else>
            <UTextarea v-model="commit.message" :rows="4" autoresize class="w-full font-mono text-xs" />
            <div class="space-y-1">
              <p class="text-xs text-(--ui-text-muted)">{{ t('task.commitFiles', { picked: commit.picked.length, total: commit.files.length }) }}</p>
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
          <UButton color="neutral" variant="ghost" :label="t('common.cancel')" @click="commit.open = false" />
          <UButton icon="i-lucide-git-commit-horizontal" :label="t('task.commit')" :loading="commit.busy" :disabled="commit.loading || !commit.message.trim() || !commit.picked.length" @click="doCommit" />
        </div>
      </template>
    </UModal>
  </div>
</template>

