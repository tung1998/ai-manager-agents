<script setup lang="ts">
// A project's Burn (spec 2026-10-01-burn-design): its main agent runs on its
// own with full access, finding work and doing it, one piece at a time in its
// own worktree, until stopped or its time is up. This page runs it; the chat
// page shows its conversation.
interface Burn {
  id?: string, conversation_id?: string, agent_id: string, model_tier: 'strong' | 'balanced' | 'fast', max_parallel: number,
  result_mode: 'branch' | 'patch', focus: string, order: 'roadmap' | 'bugs' | 'auto', ends_at: string | null, state: 'running' | 'stopped' | 'waiting_limit' | 'draining', notify_channel_id?: string, notify_chat_id?: string,
  waiting_until?: string, started_by?: string, started_at?: string,
  review_profile_id: string
}
interface Item {
  id: string, title: string, kind: 'unfinished' | 'upgrade' | 'bug', detail: string, status: string, priority: number,
  branch: string, worktree: string, summary: string, subagents: number, cost_usd: number, updated_at: string,
  reviewed: ReviewStage[], review_note: string, review_conversations: Partial<Record<ReviewStage, string>>,
  work_conversation_id: string
}
interface AgentLite { id: string, name: string, tier: string, enabled?: boolean }
const props = defineProps<{ projectId: string }>()
const { t, dateLocale } = useLang()
const toast = useToast()
const copy = useCopy()

const base = computed(() => `/api/projects/${props.projectId}/burn`)
const { data, refresh, error: loadError } = useLiveFetch<{ burn: Burn, items: Item[], weekly_reset?: string }>(base)
const { data: agentsData } = useLiveFetch<{ agents: AgentLite[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
const burn = computed(() => data.value?.burn)
const items = computed(() => data.value?.items ?? [])
const agents = computed(() => agentsData.value?.agents ?? [])
const running = computed(() => burn.value?.state === 'running' || burn.value?.state === 'waiting_limit' || burn.value?.state === 'draining')
const draining = computed(() => burn.value?.state === 'draining')

// the time left, ticking
const now = ref(Date.now())
let clock: ReturnType<typeof setInterval> | undefined
onMounted(() => { clock = setInterval(() => { now.value = Date.now() }, 1000) })
onBeforeUnmount(() => clearInterval(clock))
const when = (iso: string) => new Date(iso).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
function left(iso: string) {
  const s = Math.max(0, Math.floor((new Date(iso).getTime() - now.value) / 1000))
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60)
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m ${s % 60}s`
}

// settings (a drawer): saved as they are
const settingsOpen = ref(false)
const form = reactive({
  agent_id: '', model_tier: 'balanced' as Burn['model_tier'], max_parallel: 1, result_mode: 'branch' as Burn['result_mode'], order: 'roadmap' as Burn['order'], focus: '',
  review_profile_id: '', notify_channel_id: '', notify_chat_id: ''
})
const { stale, reset: resync } = useDraft(burn, form, (b) => {
  Object.assign(form, {
    agent_id: b.agent_id, model_tier: b.model_tier, max_parallel: b.max_parallel, result_mode: b.result_mode, order: b.order ?? 'roadmap', focus: b.focus,
    review_profile_id: b.review_profile_id ?? '', notify_channel_id: b.notify_channel_id ?? '', notify_chat_id: b.notify_chat_id ?? ''
  })
})
const saving = ref(false)
async function saveSettings() {
  saving.value = true
  try {
    await $fetch(base.value, { method: 'PUT', body: { ...form } })
    await refresh()
    resync()
    settingsOpen.value = false
    toast.add({ title: t('burn.saved'), color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    saving.value = false
  }
}
// the agent picked is paused: saving and starting are refused until it is changed or turned on
const offAgent = computed(() => agents.value.find(a => a.id === form.agent_id && a.enabled === false))
const agentItems = computed(() => agents.value.map(a => ({ value: a.id, label: a.enabled === false ? `${a.name} (${t('burn.agentOffTag')})` : a.name })))
// review (ADR-113): the saved profile it follows, set up on its own page
const reviewStages = reviewStageKeys
const { data: profData } = useLiveFetch<{ profiles: BurnReviewProfile[] }>(() => `/api/projects/${props.projectId}/burn/review-profiles`, { lazy: true })
const profiles = computed(() => profData.value?.profiles ?? [])
// the focus: a few common ones to start from (written into the box, editable)
const focusPresets = computed(() => (['security', 'uiux', 'performance', 'tests'] as const).map(k => ({ key: k, label: t(`burn.focusPreset.${k}`), text: t(`burn.focusPreset.${k}Text`) })))
function addFocus(text: string) {
  if (form.focus.includes(text)) return
  form.focus = [form.focus.trim(), text].filter(Boolean).join('\n')
}
// where its summary goes when it stops (ADR-120): a bot of the project, its chat
interface Bot { id: string, name: string, kind: 'discord' | 'telegram' }
const { data: botsData } = useLiveFetch<{ channels: Bot[] }>(() => `/api/projects/${props.projectId}/channels`, { lazy: true })
const notifyBot = computed({ get: () => form.notify_channel_id || '__none', set: (v: string) => { form.notify_channel_id = v === '__none' ? '' : v } })
const botItems = computed(() => [{ value: '__none', label: t('burn.notify.off') }, ...(botsData.value?.channels ?? []).map(b => ({ value: b.id, label: `${b.name} · ${b.kind === 'discord' ? 'Discord' : 'Telegram'}` }))])
const notifyKind = computed(() => botsData.value?.channels.find(b => b.id === form.notify_channel_id)?.kind)
const profileItems = computed(() => [{ value: '__none', label: t('burn.review.none') }, ...profiles.value.map(p => ({ value: p.id, label: p.name }))])
const reviewProfile = computed({ get: () => form.review_profile_id || '__none', set: (v: string) => { form.review_profile_id = v === '__none' ? '' : v } })
const profile = computed(() => profiles.value.find(p => p.id === form.review_profile_id))
const reviewSummary = computed(() => profile.value ? `${profile.value.name} (${reviewStages.filter(v => profile.value!.stages[v]).map(v => t(`burn.review.${v}`)).join(', ')})` : '')
// a piece's own chats, hidden, read here: its work (ADR-116), its reviews (ADR-114)
type ChatTab = ReviewStage | 'work'
const reviewingId = ref<string | null>(null)
const reviewing = computed(() => items.value.find(i => i.id === reviewingId.value) ?? null)
const reviewTab = ref<ChatTab>('work')
const chatOf = (it: Item | null, v: ChatTab) => v === 'work' ? it?.work_conversation_id : it?.review_conversations?.[v]
const chatTabs = (it: Item | null): ChatTab[] => (['work', ...reviewStages] as ChatTab[]).filter(v => chatOf(it, v))
const reviewTabs = computed(() => chatTabs(reviewing.value).map(v => ({ value: v, label: t(v === 'work' ? 'burn.workChat' : `burn.review.${v}`) })))
const hasReviews = (it: Item) => chatTabs(it).length > 0
function openReviews(it: Item) {
  reviewingId.value = it.id
  reviewTab.value = chatTabs(it).at(-1) ?? 'work' // the latest
}
const profilesPage = computed(() => `/projects/${props.projectId}/burn/reviews`)
const tierItems = computed(() => (['strong', 'balanced', 'fast'] as const).map(v => ({ value: v, label: t(`burn.tier.${v}`) })))

// starting asks first: what it means, and when it stops
const confirmOpen = ref(false)
const endMode = ref<'reset' | 'hours' | 'at' | 'none'>('reset')
const endHours = ref(4)
const endAt = ref('')
function openStart() {
  endMode.value = data.value?.weekly_reset ? 'reset' : 'hours'
  const d = new Date(Date.now() + 4 * 3600e3)
  endAt.value = new Date(d.getTime() - d.getTimezoneOffset() * 60e3).toISOString().slice(0, 16)
  confirmOpen.value = true
}
const endsAt = computed<string | null>(() => {
  switch (endMode.value) {
    case 'reset': return data.value?.weekly_reset ?? null
    case 'hours': return new Date(Date.now() + Math.max(1, endHours.value) * 3600e3).toISOString()
    case 'at': return endAt.value ? new Date(endAt.value).toISOString() : null
    default: return null
  }
})
const acting = ref('')
async function start() {
  acting.value = 'start'
  try {
    await $fetch(`${base.value}/start`, { method: 'POST', body: { ...form, ends_at: endsAt.value ?? undefined, no_end: endMode.value === 'none' } })
    confirmOpen.value = false
    await refresh()
    toast.add({ title: t('burn.started'), color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    acting.value = ''
  }
}
// stop: drain (finish what is in progress), resume, or stop now
async function stop(how: 'drain' | 'resume' | 'stop') {
  acting.value = how
  try {
    await $fetch(`${base.value}/${how}`, { method: 'POST' })
    await refresh()
    toast.add({ title: t(how === 'drain' ? 'burn.draining' : how === 'resume' ? 'burn.resumed' : 'burn.stopped'), color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    acting.value = ''
  }
}

// the board
// a column shows its first colMax, the rest on asking
const colMax = 10
const expanded = ref(new Set<string>())
function expand(key: string) { expanded.value = new Set([...expanded.value, key]) }
const columns = computed(() => [
  { key: 'found', label: t('burn.col.found'), statuses: ['found'] },
  { key: 'doing', label: t('burn.col.doing'), statuses: ['doing', 'review', 'queued'] },
  { key: 'paused', label: t('burn.col.paused'), statuses: ['paused'] },
  { key: 'done', label: t('burn.col.done'), statuses: ['done'] },
  { key: 'other', label: t('burn.col.other'), statuses: ['failed', 'skipped'] }
].map((c) => {
  const all = items.value.filter(i => c.statuses.includes(i.status))
  return { ...c, all, items: expanded.value.has(c.key) ? all : all.slice(0, colMax) }
}))
const kindColor = (k: Item['kind']) => ({ bug: 'error', unfinished: 'warning', upgrade: 'info' } as const)[k]
const itemActing = ref('')
async function itemAction(it: Item, action: 'skip' | 'first' | 'drop-worktree') {
  if (itemActing.value) return
  if (action === 'drop-worktree' && !confirm(t('burn.dropConfirm', { branch: it.branch }))) return
  itemActing.value = it.id + action
  try {
    await $fetch(`/api/burn-items/${it.id}/${action}`, { method: 'POST' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    itemActing.value = ''
  }
}
const totalCost = computed(() => items.value.reduce((n, i) => n + i.cost_usd, 0))
</script>

<template>
  <div class="space-y-4">
    <UAlert v-if="loadError" color="error" variant="subtle" icon="i-lucide-triangle-alert" :title="t('common.loadError')" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
    <UCard :ui="{ body: 'space-y-3 sm:p-4' }">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
        <p class="flex items-center gap-2 font-semibold">
          <UIcon name="i-lucide-flame" class="size-5" :class="running ? 'text-(--ui-warning)' : 'text-(--ui-text-muted)'" />{{ t('burn.title') }}
        </p>
        <UBadge v-if="burn" :label="t(`burn.state.${burn.state}`)" :color="burn.state === 'running' ? 'warning' : burn.state === 'waiting_limit' || burn.state === 'draining' ? 'info' : 'neutral'" variant="subtle" />
        <span v-if="running && burn?.ends_at" class="text-xs tabular-nums text-(--ui-text-muted)">{{ t('burn.endsIn', { left: left(burn.ends_at), at: when(burn.ends_at) }) }}</span>
        <span v-else-if="running" class="text-xs text-(--ui-text-muted)">{{ t('burn.noEnd') }}</span>
        <span v-if="burn?.state === 'waiting_limit' && burn.waiting_until" class="text-xs text-(--ui-text-muted)">{{ t('burn.waitingUntil', { at: when(burn.waiting_until) }) }}</span>
        <div class="ms-auto flex flex-wrap items-center gap-1.5">
          <UButton v-if="burn?.conversation_id" size="sm" color="neutral" variant="ghost" icon="i-lucide-messages-square" :label="t('burn.openChat')" :to="`/projects/${projectId}?tab=chat&c=${burn.conversation_id}`" />
          <UButton size="sm" color="neutral" variant="outline" icon="i-lucide-settings-2" :label="t('burn.settings')" @click="settingsOpen = true" />
          <template v-if="draining">
            <UButton size="sm" color="warning" variant="outline" icon="i-lucide-play" :label="t('burn.resume')" :loading="acting === 'resume'" @click="stop('resume')" />
            <UButton size="sm" color="neutral" icon="i-lucide-square" :label="t('burn.stopNow')" :loading="acting === 'stop'" @click="stop('stop')" />
          </template>
          <UButton v-else-if="running" size="sm" color="neutral" icon="i-lucide-square" :label="t('burn.stop')" :loading="acting === 'drain'" @click="stop('drain')" />
          <UButton v-else size="sm" color="warning" icon="i-lucide-flame" :label="t('burn.start')" @click="openStart" />
        </div>
      </div>
      <p class="text-xs text-(--ui-text-muted)">
        {{ t('burn.summary', { tier: t(`burn.tier.${form.model_tier}`), n: form.max_parallel, mode: t(`burn.mode.${form.result_mode}`) }) }} · {{ t(`burn.order.${form.order}`) }}
        <template v-if="reviewSummary"> · {{ t('burn.review.summary', { profile: reviewSummary }) }}</template>
        <template v-if="items.length"> · {{ t('burn.cost', { usd: totalCost.toFixed(2), n: items.filter(i => i.status === 'done').length }) }}</template>
      </p>
      <UAlert v-if="offAgent" color="warning" variant="subtle" icon="i-lucide-power-off" :title="t('burn.agentOff', { name: offAgent.name })"
        :actions="[{ label: t('burn.agentOffChange'), color: 'warning', variant: 'outline', onClick: () => { settingsOpen = true } }, { label: t('burn.agentOffOpen'), color: 'neutral', variant: 'ghost', to: `/projects/${projectId}/agents/${offAgent.id}` }]" />
      <p v-if="form.focus" class="text-xs"><span class="text-(--ui-text-muted)">{{ t('burn.focus') }}:</span> {{ form.focus }}</p>
    </UCard>

    <!-- the pieces of work, by where they are -->
    <!-- side by side on every screen: scrolled across below xl -->
    <div class="flex snap-x snap-mandatory items-start gap-3 overflow-x-auto pb-2 xl:grid xl:grid-cols-5 xl:overflow-visible xl:pb-0">
      <div v-for="col in columns" :key="col.key" class="w-[80%] min-w-0 shrink-0 snap-start space-y-2 rounded-xl border border-(--ui-border) p-2 sm:w-[45%] xl:w-auto">
        <p class="flex items-center gap-2 px-1 text-xs font-medium text-(--ui-text-muted)">{{ col.label }}<UBadge :label="String(col.all.length)" color="neutral" variant="subtle" size="sm" /></p>
        <p v-if="!col.items.length" class="px-1 py-2 text-xs text-(--ui-text-dimmed)">—</p>
        <div v-for="it in col.items" :key="it.id" class="space-y-1.5 rounded-lg bg-(--ui-bg-elevated)/60 p-2.5 text-sm">
          <div class="flex items-start gap-2">
            <UIcon v-if="it.status === 'doing'" name="i-lucide-loader-circle" class="mt-0.5 size-4 shrink-0 animate-spin text-(--ui-warning)" />
            <UIcon v-else-if="it.status === 'review'" name="i-lucide-scan-eye" class="mt-0.5 size-4 shrink-0 text-(--ui-info)" :title="t('burn.review.waiting')" />
            <span class="min-w-0 flex-1 font-medium">{{ it.title }}</span>
            <UBadge :label="t(`burn.kind.${it.kind}`)" :color="kindColor(it.kind)" variant="subtle" size="sm" />
          </div>
          <p v-if="it.summary" class="line-clamp-4 whitespace-pre-line text-xs text-(--ui-text-muted)">{{ it.summary }}</p>
          <p v-else-if="it.detail" class="line-clamp-3 text-xs text-(--ui-text-muted)">{{ it.detail }}</p>
          <p v-if="it.review_note" class="flex gap-1 text-xs text-(--ui-text-muted)" :title="it.review_note">
            <UIcon name="i-lucide-scan-eye" class="mt-0.5 size-3.5 shrink-0" /><span class="line-clamp-2">{{ it.review_note }}</span>
          </p>
          <button v-if="it.branch && it.status !== 'found'" type="button" class="flex max-w-full items-center gap-1 truncate font-mono text-xs text-(--ui-text-toned) hover:text-(--ui-text)" :title="t('burn.copyBranch')" @click="copy(`git switch ${it.branch}`)">
            <UIcon name="i-lucide-git-branch" class="size-3.5 shrink-0" /><span class="truncate">{{ it.branch }}</span>
          </button>
          <div class="flex flex-wrap items-center gap-x-2 text-xs text-(--ui-text-dimmed)">
            <span v-if="it.cost_usd">${{ it.cost_usd.toFixed(2) }}</span>
            <span v-if="it.subagents">{{ t('burn.subagents', { n: it.subagents }) }}</span>
            <span v-if="it.status === 'review'" class="text-(--ui-info)">{{ t('burn.review.waiting') }}</span>
            <span v-else-if="it.reviewed?.length">{{ t('burn.review.passed', { stages: it.reviewed.map(v => t(`burn.review.${v}`)).join(', ') }) }}</span>
            <span>{{ when(it.updated_at) }}</span>
          </div>
          <div class="flex flex-wrap gap-1">
            <UButton v-if="hasReviews(it)" size="xs" color="neutral" variant="ghost" icon="i-lucide-scan-eye" :label="t('burn.review.view')" @click="openReviews(it)" />
            <UButton v-if="['found', 'skipped', 'failed'].includes(it.status)" size="xs" color="neutral" variant="outline" icon="i-lucide-arrow-up-to-line" :label="t('burn.first')" :loading="itemActing === it.id + 'first'" @click="itemAction(it, 'first')" />
            <UButton v-if="['found', 'queued', 'paused', 'review'].includes(it.status)" size="xs" color="neutral" variant="ghost" :label="t('burn.skip')" :loading="itemActing === it.id + 'skip'" @click="itemAction(it, 'skip')" />
            <UButton v-if="it.worktree && ['done', 'failed', 'skipped'].includes(it.status)" size="xs" color="neutral" variant="ghost" icon="i-lucide-trash-2" :label="t('burn.dropWorktree')" :loading="itemActing === it.id + 'drop-worktree'" @click="itemAction(it, 'drop-worktree')" />
          </div>
        </div>
        <UButton v-if="col.all.length > col.items.length" size="xs" color="neutral" variant="ghost" block :label="t('burn.showMore', { n: col.all.length - col.items.length })" @click="expand(col.key)" />
      </div>
    </div>

    <!-- settings -->
    <USlideover v-model:open="settingsOpen" :title="t('burn.settings')">
      <template #body>
        <div class="space-y-4">
          <StaleNotice :show="stale" @reload="resync()" />
          <UFormField :label="t('burn.agent')">
            <USelect v-model="form.agent_id" :items="agentItems" class="w-full" />
          </UFormField>
          <UAlert v-if="offAgent" color="warning" variant="subtle" icon="i-lucide-power-off" :title="t('burn.agentOff', { name: offAgent.name })"
            :actions="[{ label: t('burn.agentOffOpen'), color: 'warning', variant: 'outline', to: `/projects/${projectId}/agents/${offAgent.id}` }]" />

          <UFormField :label="t('burn.tierLabel')" :help="t('burn.tierHelp')">
            <USelect v-model="form.model_tier" :items="tierItems" class="w-full" />
          </UFormField>
          <UFormField :label="t('burn.parallelLabel')" :help="t('burn.parallelHelp')">
            <UInputNumber v-model="form.max_parallel" :min="1" :max="5" class="w-full" />
          </UFormField>
          <UFormField :label="t('burn.orderLabel')">
            <URadioGroup v-model="form.order" :items="(['roadmap', 'bugs', 'auto'] as const).map(o => ({ value: o, label: t(`burn.order.${o}`), description: t(`burn.order.${o}Help`) }))" />
          </UFormField>
          <UFormField :label="t('burn.modeLabel')">
            <URadioGroup v-model="form.result_mode" :items="[{ value: 'branch', label: t('burn.mode.branch'), description: t('burn.mode.branchHelp') }, { value: 'patch', label: t('burn.mode.patch'), description: t('burn.mode.patchHelp') }]" />
          </UFormField>
          <UFormField :label="t('burn.review.label')" :help="t('burn.review.help')">
            <div class="flex gap-2">
              <USelect v-model="reviewProfile" :items="profileItems" class="min-w-0 flex-1" />
              <UButton color="neutral" variant="outline" icon="i-lucide-sliders-horizontal" :label="t('burn.review.manage')" :to="profilesPage" />
            </div>
          </UFormField>
          <UFormField :label="t('burn.focus')" :help="t('burn.focusHelp')">
            <div class="space-y-2">
              <div class="flex flex-wrap gap-1">
                <UButton v-for="f in focusPresets" :key="f.key" size="xs" color="neutral" variant="outline" icon="i-lucide-plus" :label="f.label" @click="addFocus(f.text)" />
              </div>
              <UTextarea v-model="form.focus" :rows="4" :maxlength="2000" autoresize class="w-full" :placeholder="t('burn.focusPlaceholder')" />
            </div>
          </UFormField>
          <UFormField :label="t('burn.notify.label')" :help="t('burn.notify.help')">
            <div class="grid gap-2 sm:grid-cols-2">
              <USelect v-model="notifyBot" :items="botItems" class="w-full" />
              <UInput v-model="form.notify_chat_id" class="w-full font-mono text-xs" :disabled="!form.notify_channel_id" :placeholder="notifyKind === 'telegram' ? t('alert.chatTelegram') : t('alert.chatDiscord')" />
            </div>
          </UFormField>
        </div>
      </template>
      <template #footer>
        <UButton :label="t('common.save')" :loading="saving" :disabled="!!offAgent" @click="saveSettings" />
      </template>
    </USlideover>

    <!-- a piece's reviews, to read -->
    <USlideover :open="!!reviewing" :title="t('burn.review.viewTitle', { title: reviewing?.title ?? '' })" :ui="{ content: 'sm:max-w-2xl' }" @update:open="v => { if (!v) reviewingId = null }">
      <template #body>
        <div v-if="reviewing" class="space-y-3">
          <UTabs v-if="reviewTabs.length > 1" v-model="reviewTab" :items="reviewTabs" :content="false" size="sm" />
          <RunTranscript v-if="chatOf(reviewing, reviewTab)" :key="reviewTab" :project-id="projectId" :conversation-id="chatOf(reviewing, reviewTab)!" />
        </div>
      </template>
    </USlideover>

    <!-- starting: what it means, when it stops -->
    <UModal v-model:open="confirmOpen" :title="t('burn.confirmTitle')">
      <template #body>
        <div class="space-y-3 text-sm">
          <UAlert v-if="offAgent" color="error" variant="subtle" icon="i-lucide-power-off" :title="t('burn.agentOff', { name: offAgent.name })" />
          <UAlert color="warning" variant="subtle" icon="i-lucide-shield-alert" :title="t('burn.confirmWarn')" :description="t('burn.confirmDesc', { mode: t(`burn.mode.${form.result_mode}`) })" />
          <UFormField :label="t('burn.endLabel')">
            <URadioGroup
              v-model="endMode"
              :items="[
                ...(data?.weekly_reset ? [{ value: 'reset', label: t('burn.endReset', { at: when(data.weekly_reset) }) }] : []),
                { value: 'hours', label: t('burn.endHours') },
                { value: 'at', label: t('burn.endAt') },
                { value: 'none', label: t('burn.endNone') }
              ]"
            />
          </UFormField>
          <UInputNumber v-if="endMode === 'hours'" v-model="endHours" :min="1" :max="168" class="w-40" />
          <UInput v-if="endMode === 'at'" v-model="endAt" type="datetime-local" class="w-60" />
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" :label="t('common.cancel')" @click="confirmOpen = false" />
          <UButton color="warning" icon="i-lucide-flame" :label="t('burn.confirmStart')" :loading="acting === 'start'" :disabled="!!offAgent" @click="start" />
        </div>
      </template>
    </UModal>
  </div>
</template>
