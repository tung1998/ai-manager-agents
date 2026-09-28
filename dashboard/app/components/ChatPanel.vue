<script setup lang="ts">
import type { Patch } from './PatchCard.vue'
import type { ProposedAction } from './ActionCard.vue'
import type { Attachment } from './PromptInput.vue'

interface ToolCall { name: string, summary: string, error?: boolean }
interface Message {
  id: string
  role: 'user' | 'assistant' | 'error'
  content: string
  tools: ToolCall[]
  attachments?: Attachment[]
  author: string
  created_at: string
  patches: Patch[]
  actions?: ProposedAction[]
  cost_usd?: number
}
interface Conversation { id: string, agent_id: string, agent_name: string, title: string, updated_at: string, active_turn?: string, mode?: PermLevel, edit_mode?: 'worktree' | 'direct', context_tokens?: number, context_window?: number }
interface ChatEvent { seq: number, type: 'text' | 'tool' | 'status' | 'patch' | 'done' | 'error', text?: string, tool?: ToolCall, patch?: Patch, message?: Message }

// taskId: the follow-up talk about one task (a single thread, no thread list)
// purpose "automation": the chat that builds one automation (ADR-042), made on
// first send or opened by automationId; its answers may fill the form.
// compact: no thread column (a picker instead), fills its container.
// pageContext: what the person is looking at, sent with each message.
const props = defineProps<{ projectId: string, taskId?: string, purpose?: 'automation', automationId?: string, compact?: boolean, pageContext?: () => string }>()
const emit = defineEmits<{ 'turn-done': [], 'automation-patch': [Record<string, unknown>], 'conversation': [string] }>()
const single = computed(() => !!props.taskId || props.purpose === 'automation')
const toast = useToast()
const { t, dateLocale } = useLang()

const { data: agentsData } = await useFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`)
const { data: convData, refresh: refreshConvs } = await useFetch<{ conversations: Conversation[] }>(() => `/api/projects/${props.projectId}/conversations`, { immediate: !single.value })
// every agent of the project: the person picks who answers, by its rights
const agents = computed(() => agentsData.value?.agents ?? [])
const conversations = computed(() => convData.value?.conversations ?? [])

const current = ref<Conversation | null>(null)
const messages = ref<Message[]>([])
const draft = ref('')
const draftFiles = ref<Attachment[]>([])
const editMode = ref<'worktree' | 'direct'>('worktree')
// who answers: each agent's own rights decide what it may do (no separate mode)
const pick = ref('')
watch([() => current.value?.id, agents], () => {
  editMode.value = current.value?.edit_mode ?? 'worktree'
  pick.value = current.value?.agent_id || pick.value || agents.value.find(a => a.tier === 'lead')?.id || agents.value[0]?.id || ''
}, { immediate: true })
const picked = computed(() => agents.value.find(a => a.id === pick.value))
const agentItems = computed(() => agents.value.map(a => ({ label: `${a.name} · ${permOf(agentLevel(a.permissions)).label}`, value: a.id, icon: permOf(agentLevel(a.permissions)).icon })))
const pickedLevel = computed(() => picked.value ? agentLevel(picked.value.permissions) : 'read')
const prompt = ref<{ busy: boolean } | null>(null)

// filled by other tabs (e.g. "Hỏi agent" in Vận hành)
// agentId opens a new chat with that agent, conversationId opens that chat (the agent page)
const prefill = useState<{ text: string, files: Attachment[], send?: boolean, agentId?: string, conversationId?: string } | null>('chat-prefill', () => null)
let tookPrefill = false // it already opened what it was given: the first chat / the URL's does not
async function takePrefill() {
  if (!prefill.value || single.value) return // a task's or an automation's own chat takes no prefill
  const p = prefill.value
  prefill.value = null
  tookPrefill = true
  if (p.conversationId) return open({ id: p.conversationId } as Conversation)
  if (p.agentId && !p.send) {
    stopStream()
    current.value = null
    messages.value = []
    return newConversation(p.agentId)
  }
  draft.value = p.text
  draftFiles.value = p.files
  if (p.send) {
    stopStream()
    current.value = null
    messages.value = []
    await newConversation()
    await send()
  }
}
onMounted(takePrefill)
watch(prefill, takePrefill)
const listEl = ref<HTMLElement | null>(null)

// live answer being streamed
const streaming = ref(false)
const liveText = ref('')
const liveTools = ref<ToolCall[]>([])
const liveStatus = ref('')
let source: EventSource | null = null
let turnId = ''

async function scrollDown() {
  await nextTick()
  listEl.value?.scrollTo({ top: listEl.value.scrollHeight, behavior: 'smooth' })
}

// after an answer: the conversation's context and the connection's usage changed
const { refresh: refreshLimits } = useLimits()
async function afterTurn() {
  refreshLimits(true)
  const id = current.value?.id
  if (!id) return
  try {
    const res = await $fetch<{ conversation: Conversation }>(`/api/conversations/${id}`)
    if (current.value?.id === id) current.value = { ...current.value, context_tokens: res.conversation.context_tokens, context_window: res.conversation.context_window }
  } catch { /* the next open shows it */ }
}

// the open chat (and a message) live in the URL, so a link points at them;
// only the project's Chat page (not the corner chat, a task's or a builder's)
const route = useRoute()
const router = useRouter()
const ownsUrl = computed(() => !single.value && !props.compact)
const copy = useCopy()
const chatLink = (id: string, messageId?: string) => `${location.origin}/projects/${props.projectId}?tab=chat&c=${id}${messageId ? `&m=${messageId}` : ''}`
function threadMenu(c: Conversation) {
  return [[
    { label: t('chat.copyLink'), icon: 'i-lucide-link', onSelect: () => copy(chatLink(c.id)) },
    { label: t('chat.copyId'), icon: 'i-lucide-hash', onSelect: () => copy(c.id) }
  ], [{ label: t('chat.delete'), icon: 'i-lucide-trash-2', color: 'error' as const, onSelect: () => remove(c) }]]
}
function messageMenu(m: Message) {
  const items = [{ label: t('chat.copyText'), icon: 'i-lucide-copy', onSelect: () => copy(m.content) }]
  if (current.value) items.push({ label: t('chat.copyMessageLink'), icon: 'i-lucide-link', onSelect: () => copy(chatLink(current.value!.id, m.id)) })
  return [items]
}
const marked = ref('') // the message a link points at
async function showMessage(id: string) {
  await nextTick()
  const el = document.getElementById(`m-${id}`)
  if (!el) return
  el.scrollIntoView({ block: 'center' })
  marked.value = id
  setTimeout(() => { if (marked.value === id) marked.value = '' }, 2500)
}

async function open(c: Conversation, messageId?: string) {
  stopStream()
  current.value = c
  if (ownsUrl.value && route.query.c !== c.id) router.replace({ query: { ...route.query, c: c.id, m: undefined } })
  const res = await $fetch<{ conversation: Conversation, messages: Message[] }>(`/api/conversations/${c.id}`)
  messages.value = res.messages
  current.value = res.conversation
  if (res.conversation.active_turn) follow(res.conversation.active_turn)
  if (messageId) showMessage(messageId)
  else scrollDown()
}

async function newConversation(agentId = '') {
  if (props.taskId) return openTask()
  try {
    const res = await $fetch<{ conversation: Conversation }>(`/api/projects/${props.projectId}/conversations`, { method: 'POST', body: { agent_id: agentId, purpose: props.purpose ?? '' } })
    if (props.purpose) emit('conversation', res.conversation.id)
    else await refreshConvs()
    await open(res.conversation)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

async function openTask() {
  try {
    const res = await $fetch<{ conversation: Conversation }>(`/api/tasks/${props.taskId}/conversation`, { method: 'POST' })
    await open(res.conversation)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

async function send() {
  const text = draft.value.trim()
  if ((!text && !draftFiles.value.length) || streaming.value || prompt.value?.busy) return
  if (!current.value) await newConversation(pick.value)
  if (!current.value) return
  const switching = !single.value && pick.value && pick.value !== current.value.agent_id ? pick.value : ''
  try {
    // mode operate: the agent's own rights are the limit (members are capped server-side)
    const res = await $fetch<{ turn_id: string, message: Message }>(`/api/conversations/${current.value.id}/messages`, { method: 'POST', body: { text, attachments: draftFiles.value.map(a => a.id), mode: 'operate', edit_mode: editMode.value, agent_id: switching, context: props.pageContext?.() ?? '' } })
    if (switching && picked.value && current.value) current.value = { ...current.value, agent_id: picked.value.id, agent_name: picked.value.name }
    draft.value = ''
    draftFiles.value = []
    const first = !messages.value.length
    messages.value.push(res.message)
    if (first) refreshConvs() // the server titles a conversation from its first message
    follow(res.turn_id)
    scrollDown()
  } catch (e) {
    const d = (e as { data?: { code?: string, error?: string } }).data
    if (d?.code === 'budget') {
      toast.add({ title: t('chat.budgetHit'), description: d.error, color: 'warning', actions: [{ label: t('chat.seeCosts'), onClick: () => { navigateTo('/costs') } }] })
    } else {
      toast.add({ title: apiError(e), color: 'error' })
    }
  }
}

function follow(id: string) {
  stopStream()
  turnId = id
  streaming.value = true
  liveText.value = ''
  liveTools.value = []
  liveStatus.value = ''
  source = new EventSource(`/api/chat/turns/${id}/stream`)
  source.onmessage = (m) => {
    const ev = JSON.parse(m.data) as ChatEvent
    switch (ev.type) {
      case 'status': liveStatus.value = ev.text ?? ''; break
      case 'text': liveText.value += ev.text ?? ''; scrollDown(); break
      case 'tool': if (ev.tool) liveTools.value.push(ev.tool); scrollDown(); break
      case 'done':
      case 'error':
        if (ev.message) {
          messages.value.push(ev.message)
          if (props.purpose === 'automation' && ev.type === 'done') automationBlocks(ev.message.content).forEach(p => emit('automation-patch', p))
        }
        finishStream()
        afterTurn()
        if (props.taskId) emit('turn-done')
        else if (!single.value) refreshConvs()
        scrollDown()
        break
    }
  }
  source.onerror = () => {
    // the turn is gone (finished before we connected): reload the thread
    if (source?.readyState === EventSource.CLOSED) {
      finishStream()
      if (current.value) open(current.value)
    }
  }
}

function finishStream() {
  source?.close()
  source = null
  streaming.value = false
  liveText.value = ''
  liveTools.value = []
}
function stopStream() {
  source?.close()
  source = null
  streaming.value = false
}
async function cancel() {
  if (turnId) await $fetch(`/api/chat/turns/${turnId}/cancel`, { method: 'POST', body: {} }).catch(() => {})
}

async function remove(c: Conversation) {
  if (!confirm(t('chat.deleteConfirm'))) return
  await $fetch(`/api/conversations/${c.id}`, { method: 'DELETE' })
  if (current.value?.id === c.id) {
    current.value = null
    messages.value = []
    if (ownsUrl.value) router.replace({ query: { ...route.query, c: undefined, m: undefined } })
  }
  refreshConvs()
}

function onPatchUpdated(msg: Message, p: Patch) {
  const i = msg.patches.findIndex(x => x.id === p.id)
  if (i >= 0) msg.patches[i] = p
}

const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })

// ```automation {…}``` blocks of an answer: form changes (bad JSON is skipped)
function automationBlocks(text: string): Record<string, unknown>[] {
  const out: Record<string, unknown>[] = []
  for (const m of text.matchAll(/```automation\s*\n([\s\S]*?)```/g)) {
    try {
      const v = JSON.parse(m[1]!)
      if (v && typeof v === 'object' && !Array.isArray(v)) out.push(v)
    } catch { /* not JSON: ignore */ }
  }
  return out
}

async function openAutomation() {
  if (!props.automationId) return
  try {
    const res = await $fetch<{ conversation: Conversation }>(`/api/automations/${props.automationId}/conversation`, { method: 'POST', body: {} })
    emit('conversation', res.conversation.id)
    await open(res.conversation)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
const threadItems = computed(() => conversations.value.map(c => ({ label: c.title || t('chat.untitled'), value: c.id })))
const threadPick = computed({
  get: () => current.value?.id,
  set: (id?: string) => { const c = conversations.value.find(x => x.id === id); if (c) open(c) }
})

onMounted(() => {
  if (props.taskId) openTask()
  else if (props.purpose === 'automation') openAutomation()
  else if (ownsUrl.value && typeof route.query.c === 'string' && !tookPrefill) open({ id: route.query.c } as Conversation, typeof route.query.m === 'string' ? route.query.m : undefined)
  else if (conversations.value[0] && !tookPrefill) open(conversations.value[0])
})
onBeforeUnmount(stopStream)
</script>

<template>
  <div class="flex overflow-hidden rounded-lg border border-(--ui-border)" :class="compact || purpose ? 'h-full min-h-0' : taskId ? 'h-[32rem]' : 'min-h-[24rem] flex-1'">
    <!-- threads -->
    <aside v-if="!single && !compact" class="hidden w-60 shrink-0 flex-col border-e border-(--ui-border) md:flex">
      <div class="flex items-center gap-1 border-b border-(--ui-border) p-2">
        <UButton icon="i-lucide-square-pen" :label="t('chat.newThread')" size="sm" color="neutral" variant="ghost" class="flex-1 justify-start" @click="newConversation(pick)" />
      </div>
      <div class="flex-1 overflow-y-auto p-1">
        <p v-if="!conversations.length" class="p-3 text-xs text-(--ui-text-muted)">{{ t('chat.none') }}</p>
        <div
          v-for="c in conversations" :key="c.id"
          class="group flex cursor-pointer items-start gap-1 rounded-md px-2 py-1.5 text-sm"
          :class="current?.id === c.id ? 'bg-(--ui-bg-accented)' : 'hover:bg-(--ui-bg-muted)'"
          @click="open(c)"
        >
          <div class="min-w-0 flex-1">
            <p class="truncate">{{ c.title || t('chat.newThreadTitle') }}</p>
            <p class="truncate text-xs text-(--ui-text-muted)">{{ c.agent_name }} · {{ when(c.updated_at) }}</p>
          </div>
          <UIcon v-if="c.active_turn" name="i-lucide-loader-circle" class="mt-1 size-3.5 animate-spin text-(--ui-text-muted)" />
          <UDropdownMenu :items="threadMenu(c)" :content="{ align: 'end' }">
            <button type="button" class="invisible -me-1 rounded px-0.5 text-(--ui-text-dimmed) hover:text-(--ui-text) group-hover:visible data-[state=open]:visible" :aria-label="t('chat.more')" @click.stop>
              <UIcon name="i-lucide-ellipsis" class="size-4" />
            </button>
          </UDropdownMenu>
        </div>
      </div>
    </aside>

    <!-- thread -->
    <section class="flex min-w-0 flex-1 flex-col">
      <div v-if="compact && !single" class="flex items-center gap-1 border-b border-(--ui-border) p-2">
        <USelect v-model="threadPick" :items="threadItems" size="xs" class="min-w-0 flex-1" :placeholder="t('chat.newThread')" />
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-plus" :aria-label="t('chat.newThread')" @click="newConversation()" />
      </div>
      <div ref="listEl" class="flex-1 space-y-4 overflow-y-auto p-4">
        <div v-if="!messages.length && !streaming" class="flex h-full flex-col items-center justify-center gap-2 text-center text-(--ui-text-muted)">
          <UIcon name="i-lucide-messages-square" class="size-8" />
          <template v-if="taskId">
            <p class="text-sm">{{ t('chat.askAboutTask', { agent: current?.agent_name || t('chat.sendManager') }) }}</p>
            <p class="text-xs">{{ t('chat.taskPatchHint') }}</p>
          </template>
          <template v-else-if="purpose === 'automation'">
            <p class="text-sm">{{ t('chat.askAboutAutomation') }}</p>
            <p class="text-xs">{{ t('chat.automationHint') }}</p>
          </template>
          <template v-else>
            <p class="text-sm">{{ t('chat.askAboutProject') }}</p>
            <p class="text-xs">{{ t('chat.readOnlyHint') }}</p>
          </template>
        </div>

        <template v-for="m in messages" :key="m.id">
          <div v-if="m.role === 'user'" :id="`m-${m.id}`" class="group/msg flex flex-col items-end gap-1.5 rounded-lg transition" :class="marked === m.id && 'ring-2 ring-primary/60 ring-offset-4 ring-offset-(--ui-bg)'">
            <AttachmentList :items="m.attachments ?? []" align="end" />
            <div class="flex max-w-[80%] items-start gap-1">
              <UDropdownMenu :items="messageMenu(m)" :content="{ align: 'end' }">
                <button type="button" class="invisible mt-1.5 rounded px-0.5 text-(--ui-text-dimmed) hover:text-(--ui-text) group-hover/msg:visible data-[state=open]:visible" :aria-label="t('chat.more')">
                  <UIcon name="i-lucide-ellipsis" class="size-4" />
                </button>
              </UDropdownMenu>
              <div v-if="m.content" class="min-w-0 whitespace-pre-wrap rounded-2xl rounded-br-sm bg-(--ui-primary) px-3.5 py-2 text-sm text-white">{{ m.content }}</div>
            </div>
          </div>
          <div v-else-if="m.role === 'error'" class="flex items-start gap-2 text-sm text-(--ui-error)">
            <UIcon name="i-lucide-circle-alert" class="mt-0.5 size-4 shrink-0" />
            <span>{{ m.content }}</span>
          </div>
          <div v-else :id="`m-${m.id}`" class="group/msg space-y-2 rounded-lg transition" :class="marked === m.id && 'ring-2 ring-primary/60 ring-offset-4 ring-offset-(--ui-bg)'">
            <div class="flex items-center gap-2 text-xs text-(--ui-text-muted)">
              <UIcon name="i-lucide-bot" class="size-4 text-primary" />
              <span class="font-medium">{{ m.author }}</span>
              <span>{{ when(m.created_at) }}</span>
              <span v-if="m.cost_usd">· ${{ m.cost_usd.toFixed(3) }}</span>
              <UDropdownMenu :items="messageMenu(m)" :content="{ align: 'start' }">
                <button type="button" class="invisible rounded px-0.5 text-(--ui-text-dimmed) hover:text-(--ui-text) group-hover/msg:visible data-[state=open]:visible" :aria-label="t('chat.more')">
                  <UIcon name="i-lucide-ellipsis" class="size-4" />
                </button>
              </UDropdownMenu>
            </div>
            <details v-if="m.tools.length" class="text-xs text-(--ui-text-muted)">
              <summary class="cursor-pointer">{{ t('chat.toolsUsed', { n: m.tools.length }) }}</summary>
              <ul class="mt-1 space-y-0.5 ps-4">
                <li v-for="(t, i) in m.tools" :key="i" :class="t.error ? 'text-(--ui-error)' : ''">{{ t.summary }}</li>
              </ul>
            </details>
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div class="markdown text-sm" v-html="renderMarkdown(m.content)" />
            <PatchCard v-for="p in m.patches" :key="p.id" :patch="p" @updated="(np: Patch) => onPatchUpdated(m, np)" />
            <ActionCard
              v-for="a in m.actions ?? []" :key="a.id" :action="a" :project-id="projectId"
              @updated="(na: ProposedAction) => m.actions = (m.actions ?? []).map(x => x.id === na.id ? na : x)"
            />
          </div>
        </template>

        <div v-if="streaming" class="space-y-2">
          <div class="flex items-center gap-2 text-xs text-(--ui-text-muted)">
            <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin text-primary" />
            <span>{{ liveStatus || t('chat.replying') }}</span>
          </div>
          <ul v-if="liveTools.length" class="space-y-0.5 ps-6 text-xs text-(--ui-text-muted)">
            <li v-for="(t, i) in liveTools" :key="i">{{ t.summary }}</li>
          </ul>
          <!-- eslint-disable-next-line vue/no-v-html -->
          <div v-if="liveText" class="markdown text-sm" v-html="renderMarkdown(liveText)" />
        </div>
      </div>

      <form class="border-t border-(--ui-border) p-3" @submit.prevent="send">
        <PromptInput
          ref="prompt" v-model="draft" v-model:attachments="draftFiles" :project-id="projectId"
          :placeholder="picked ? t('chat.placeholderWithAgent', { agent: picked.name }) : t('chat.placeholderNoAgent')"
          @submit="send"
        >
          <template #actions>
            <ContextMeter :tokens="current?.context_tokens" :window="current?.context_window" />
            <USelect
              v-if="!single && agents.length" v-model="pick" :items="agentItems" size="sm" variant="ghost" class="max-w-56"
              :icon="permOf(pickedLevel).icon" :title="permOf(pickedLevel).description" :aria-label="t('chat.pickAgent')"
            />
            <EditModePicker v-if="permRank(pickedLevel) >= permRank('propose')" v-model="editMode" />
            <UButton v-if="streaming" size="sm" icon="i-lucide-square" color="neutral" variant="outline" :label="t('chat.stop')" @click="cancel" />
            <UButton v-else size="sm" type="submit" icon="i-lucide-send" :disabled="!draft.trim() && !draftFiles.length" />
          </template>
        </PromptInput>
      </form>
    </section>
  </div>
</template>

<style scoped>
.markdown :deep(p) { margin: 0.4rem 0; }
.markdown :deep(ul) { list-style: disc; padding-inline-start: 1.25rem; margin: 0.4rem 0; }
.markdown :deep(ol) { list-style: decimal; padding-inline-start: 1.25rem; margin: 0.4rem 0; }
.markdown :deep(code) { font-family: ui-monospace, monospace; font-size: 0.85em; background: var(--ui-bg-muted); padding: 0.1rem 0.3rem; border-radius: 0.25rem; }
.markdown :deep(pre) { background: var(--ui-bg-muted); padding: 0.75rem; border-radius: 0.5rem; overflow-x: auto; margin: 0.5rem 0; }
.markdown :deep(pre code) { background: none; padding: 0; }
.markdown :deep(h1), .markdown :deep(h2), .markdown :deep(h3) { font-weight: 600; margin: 0.75rem 0 0.25rem; }
.markdown :deep(a) { color: var(--ui-primary); text-decoration: underline; }
.markdown :deep(table) { border-collapse: collapse; margin: 0.5rem 0; font-size: 0.85em; }
.markdown :deep(th), .markdown :deep(td) { border: 1px solid var(--ui-border); padding: 0.25rem 0.5rem; }
</style>
