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
interface Conversation { id: string, agent_id: string, agent_name: string, title: string, updated_at: string, source?: Source, purpose?: string, external_url?: string, active_turn?: string, mode?: PermLevel, edit_mode?: 'worktree' | 'direct', context_tokens?: number, context_window?: number }
interface ChatEvent { seq: number, type: 'text' | 'tool' | 'status' | 'patch' | 'done' | 'error', text?: string, tool?: ToolCall, patch?: Patch, message?: Message, next_turn_id?: string }
// the agents in a chat and the answers in progress (ADR-044)
interface Member { agent_id: string, agent_name: string, level: string, context_tokens: number, context_window: number }
interface RunningTurn { turn_id: string, agent_name: string, background: boolean }

// purpose "automation": the chat that builds one automation (ADR-042), made on
// first send or opened by automationId; its answers may fill the form.
// compact: no thread column (a picker instead), fills its container.
// pageContext: what the person is looking at, sent with each message.
const props = defineProps<{ projectId: string, purpose?: 'automation' | 'skill' | 'template', automationId?: string, compact?: boolean, pageContext?: () => string }>()
const emit = defineEmits<{ 'automation-patch': [Record<string, unknown>], 'skill-patch': [Record<string, unknown>], 'template-patch': [Record<string, unknown>], 'history': [Record<string, unknown>[]], 'conversation': [string], 'back': [] }>()
const single = computed(() => !!props.purpose)
// the chat page on a phone: the input stays behind a button until asked for,
// so the messages get the whole screen
const page = computed(() => !props.compact && !single.value)
const composeOpen = ref(false)
const toast = useToast()
const { t, dateLocale } = useLang()

const _f1 = useLiveFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
const { data: agentsData } = _f1
// where the chats started: the dashboard, a bot, an automation
const origin = ref<'all' | Source>('all')
const _f2 = useLiveFetch<{ conversations: Conversation[] }>(() => `/api/projects/${props.projectId}/conversations?source=${origin.value}`, { immediate: !single.value, lazy: true })
const { data: convData, refresh: refreshConvs, pending: convsLoading } = _f2
// not awaited: the chat shows at once with its skeletons (a phone over a VPN)
// every agent of the project: the person picks who answers, by its rights
const agents = computed(() => agentsData.value?.agents ?? [])
const conversations = computed(() => convData.value?.conversations ?? [])

const current = ref<Conversation | null>(null)
const threadsOpen = ref(false) // the chats drawer on a phone
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
const prompt = ref<{ busy: boolean, focus?: () => void } | null>(null)

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

// older messages, a page at a time, the reading place kept
const PAGE = 20
const hasOlder = ref(false)
const loadingOlder = ref(false)
async function loadOlder() {
  const c = current.value
  const first = messages.value[0]
  if (!c || !first || loadingOlder.value) return
  loadingOlder.value = true
  try {
    const el = listEl.value
    const from = el ? el.scrollHeight - el.scrollTop : 0
    const res = await $fetch<{ messages: Message[], has_more?: boolean }>(`/api/conversations/${c.id}?limit=${PAGE}&before=${first.id}`)
    messages.value = [...res.messages, ...messages.value]
    hasOlder.value = !!res.has_more
    await nextTick()
    if (el) el.scrollTop = el.scrollHeight - from
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    loadingOlder.value = false
  }
}

// what changed elsewhere (a Discord message into this chat, an agent's answer
// in the background): the list, and the open chat's latest messages
const nearEnd = () => { const el = listEl.value; return !el || el.scrollHeight - el.scrollTop - el.clientHeight < 120 }
onLiveChange(async (tables) => {
  if (!tables.includes('*') && !tables.some(t => ['messages', 'conversations', 'patches', 'actions', 'conversation_agents'].includes(t))) return // '*': back online
  if (!single.value && !tables.includes('conversations') && !tables.includes('*')) refreshConvs() // the list's own fetch refreshes on those
  const c = current.value
  if (!c || streaming.value || loadingMsgs.value) return
  try {
    const res = await $fetch<{ messages: Message[], members?: Member[], running?: RunningTurn[], conversation: Conversation }>(`/api/conversations/${c.id}?limit=${PAGE}`)
    if (current.value?.id !== c.id || streaming.value) return
    const known = new Set(messages.value.map(m => m.id))
    const fresh = res.messages.filter(m => !known.has(m.id))
    const changed = res.messages.some(m => { const x = messages.value.find(y => y.id === m.id); return x && JSON.stringify(x) !== JSON.stringify(m) })
    if (!fresh.length && !changed) return
    const stay = nearEnd()
    messages.value = [...messages.value.filter(m => !res.messages.some(n => n.id === m.id)), ...res.messages]
    applyGroup(res.members, res.running)
    if (res.conversation.active_turn && !streaming.value) follow(res.conversation.active_turn)
    if (stay) scrollDown()
  } catch { /* gone, or offline: the next change tries again */ }
})

let scrollQueued = false
async function scrollDown() { // once a frame, however many tokens came in it
  if (scrollQueued) return
  scrollQueued = true
  await nextTick()
  requestAnimationFrame(() => {
    scrollQueued = false
    listEl.value?.scrollTo({ top: listEl.value.scrollHeight, behavior: 'smooth' })
  })
}
// a chat just opened shows its last message at once (no scrolling down from
// the top); it stays there while what it shows settles (markdown, images)
async function jumpToEnd() {
  await nextTick()
  const el = listEl.value
  if (!el) return
  const pin = () => { el.scrollTop = el.scrollHeight }
  pin()
  requestAnimationFrame(pin)
  const ro = new ResizeObserver(pin)
  for (const child of Array.from(el.children)) ro.observe(child)
  const stop = () => ro.disconnect()
  el.addEventListener('wheel', stop, { once: true, passive: true }) // the person scrolls: leave them there
  el.addEventListener('touchstart', stop, { once: true, passive: true })
  setTimeout(stop, 1500)
}

// after an answer: the conversation's context and the connection's usage changed
const { refresh: refreshLimits } = useLimits()
async function afterTurn() {
  refreshLimits(true)
  const id = current.value?.id
  if (!id) return
  try {
    const res = await $fetch<{ conversation: Conversation, members?: Member[], running?: RunningTurn[] }>(`/api/conversations/${id}`)
    if (current.value?.id !== id) return
    current.value = { ...current.value, context_tokens: res.conversation.context_tokens, context_window: res.conversation.context_window }
    applyGroup(res.members, res.running)
  } catch { /* the next open shows it */ }
}

// agents working in the background on a hand-off (like subagents): their
// answers land in the thread when done; the one who asked then reports back
const members = ref<Member[]>([])
const running = ref<RunningTurn[]>([])
const background = computed(() => running.value.filter(r => r.background))
const bgSources = new Map<string, EventSource>()
function applyGroup(m?: Member[], r?: RunningTurn[]) {
  members.value = m ?? []
  running.value = r ?? []
  for (const b of background.value) {
    if (bgSources.has(b.turn_id)) continue
    const es = new EventSource(`/api/chat/turns/${b.turn_id}/stream`)
    bgSources.set(b.turn_id, es)
    es.onmessage = (msg) => {
      const ev = JSON.parse(msg.data) as ChatEvent
      if (ev.type !== 'done' && ev.type !== 'error') return
      if (ev.message && !messages.value.some(x => x.id === ev.message!.id)) messages.value.push(ev.message)
      es.close()
      bgSources.delete(b.turn_id)
      scrollDown()
      afterTurn()
    }
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        bgSources.delete(b.turn_id)
        afterTurn()
      }
    }
  }
  // the one who asked reports back: follow it when nothing else is streaming
  const fg = running.value.find(x => !x.background)
  if (fg && !streaming.value) follow(fg.turn_id)
}
function stopBackground() {
  bgSources.forEach(es => es.close())
  bgSources.clear()
}
async function cancelTurn(id: string) {
  await $fetch(`/api/chat/turns/${id}/cancel`, { method: 'POST', body: {} }).catch(() => {})
}
// the person's message with the tags of agents marked
function tagged(text: string) {
  const names = agents.value.map(a => a.name).sort((a, b) => b.length - a.length).map(n => n.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  if (!names.length) return [{ text, tag: false }]
  // split with a capture group: odd parts are the tags
  // not in the middle of a word (an email) nor inside code
  if (text.includes('`')) return [{ text, tag: false }]
  return text.split(new RegExp(`((?<![\\p{L}\\p{N}_])@(?:${names.join('|')}))`, 'iu')).map((part, i) => ({ text: part, tag: i % 2 === 1 })).filter(p => p.text)
}

// the open chat (and a message) live in the URL, so a link points at them;
// only the project's Chat page (not the corner chat, a task's or a builder's)
const route = useRoute()
const router = useRouter()
const ownsUrl = computed(() => !single.value && !props.compact)
const copy = useCopy()
const chatLink = (id: string, messageId?: string) => `${location.origin}/projects/${props.projectId}?tab=chat&c=${id}${messageId ? `&m=${messageId}` : ''}`
// unread: an agent answered since the person last looked. The open chat is
// seen (also an answer that comes while it shows), unless they marked it unread.
const unread = useUnread()
const keptUnread = ref('')
watch([() => current.value?.id, unread.ids], ([id]) => {
  if (!id || id === keptUnread.value || !unread.ids.value.has(id) || document.visibilityState !== 'visible') return
  void unread.mark(id, true).catch(() => {})
})
watch(() => current.value?.id, (id) => { if (id !== keptUnread.value) keptUnread.value = '' })
function markRead(c: Conversation, seen: boolean) {
  keptUnread.value = seen ? '' : c.id
  void unread.mark(c.id, seen).catch(e => toast.add({ title: apiError(e), color: 'error' }))
}
function threadMenu(c: Conversation) {
  const isUnread = unread.ids.value.has(c.id)
  const items: { label: string, icon: string, onSelect: () => unknown }[] = [
    { label: t('chat.copyLink'), icon: 'i-lucide-link', onSelect: () => copy(chatLink(c.id)) },
    { label: t('chat.copyId'), icon: 'i-lucide-hash', onSelect: () => copy(c.id) },
    isUnread
      ? { label: t('chat.markSeen'), icon: 'i-lucide-mail-open', onSelect: () => markRead(c, true) }
      : { label: t('chat.markUnread'), icon: 'i-lucide-mail', onSelect: () => markRead(c, false) }
  ]
  // a Discord/Telegram chat: where it is there (its thread, once it has one)
  if (c.external_url) items.unshift({ label: c.source === 'telegram' ? t('chat.openInTelegram') : t('chat.openInDiscord'), icon: 'i-lucide-external-link', onSelect: () => { window.open(c.external_url, '_blank', 'noopener') } })
  return [items, [{ label: t('chat.delete'), icon: 'i-lucide-trash-2', color: 'error' as const, onSelect: () => remove(c) }]]
}
// an agent's avatar opens onto its settings page
function agentMenu(a?: { id?: string, name?: string }) {
  if (!a?.id) return []
  return [[{ label: t('chat.agentSettings'), icon: 'i-lucide-settings', to: `/projects/${props.projectId}/agents/${a.id}` }]]
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

const loadingMsgs = ref(false) // a chat's messages on their way: its skeleton shows
async function open(c: Conversation, messageId?: string) {
  stopStream()
  if (current.value?.id !== c.id) messages.value = [] // another chat: not the last one's messages meanwhile
  current.value = c
  if (ownsUrl.value && route.query.c !== c.id) router.replace({ query: { ...route.query, c: c.id, m: undefined } })
  stopBackground()
  // the last page (a phone over a VPN); a link to one message loads it all to find it
  loadingMsgs.value = true
  let res: { conversation: Conversation, messages: Message[], has_more?: boolean, members?: Member[], running?: RunningTurn[] }
  try {
    res = await $fetch(`/api/conversations/${c.id}${messageId ? '' : `?limit=${PAGE}`}`)
  } finally {
    loadingMsgs.value = false
  }
  messages.value = res.messages
  hasOlder.value = !!res.has_more
  current.value = res.conversation
  applyGroup(res.members, res.running)
  if (res.conversation.active_turn && !streaming.value) follow(res.conversation.active_turn)
  if (messageId) showMessage(messageId)
  else jumpToEnd()
}

async function newConversation(agentId = '') {
  try {
    const res = await $fetch<{ conversation: Conversation }>(`/api/projects/${props.projectId}/conversations`, { method: 'POST', body: { agent_id: agentId, purpose: props.purpose ?? '' } })
    if (props.purpose) emit('conversation', res.conversation.id)
    else await refreshConvs()
    // the skill editor's chat is in the URL: back, reload or a link reopens it
    if (props.purpose === 'skill' || props.purpose === 'template') router.replace({ query: { ...route.query, c: res.conversation.id } })
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
      toast.add({ title: t('chat.budgetHit'), description: d.error, color: 'warning', actions: [{ label: t('chat.seeCosts'), onClick: () => { navigateTo(`/projects/${props.projectId}?tab=info`) } }] })
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
          if (!messages.value.some(x => x.id === ev.message!.id)) messages.value.push(ev.message) // the live refresh may have it already
          if (props.purpose === 'automation' && ev.type === 'done') fencedBlocks(ev.message.content, 'automation').forEach(p => emit('automation-patch', p))
          if (props.purpose === 'skill' && ev.type === 'done') fencedBlocks(ev.message.content, 'skill').forEach(p => emit('skill-patch', p))
          if (props.purpose === 'template' && ev.type === 'done') fencedBlocks(ev.message.content, 'template').forEach(p => emit('template-patch', p))
        }
        if (ev.next_turn_id) { // the next agent tagged answers now
          follow(ev.next_turn_id)
          scrollDown()
          break
        }
        finishStream()
        afterTurn()
        if (!single.value) refreshConvs()
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
  // Stop: the answer and every agent working in the background in this chat
  if (current.value && !single.value) await $fetch(`/api/conversations/${current.value.id}/stop`, { method: 'POST', body: {} }).catch(() => {})
  else if (turnId) await $fetch(`/api/chat/turns/${turnId}/cancel`, { method: 'POST', body: {} }).catch(() => {})
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

// ```automation {…}``` (or ```skill```) blocks of an answer: form changes (bad JSON is skipped)
function fencedBlocks(text: string, lang: string): Record<string, unknown>[] {
  const out: Record<string, unknown>[] = []
  // the closing fence starts a line: JSON has no raw newline, while a skill's body may hold ``` of its own
  for (const m of text.matchAll(new RegExp('```' + lang + '[ \\t]*\\n([\\s\\S]*?)\\n[ \\t]*```', 'g'))) {
    try {
      const v = JSON.parse(m[1]!)
      if (v && typeof v === 'object' && !Array.isArray(v)) out.push(v)
    } catch { /* not JSON: ignore */ }
  }
  return out
}

// a skill an agent drafted in the chat (Claude Code may not write .claude/): the
// editor opens with it, a person reviews and saves
const skillDrafts = (text: string) => text.includes('```skill') ? fencedBlocks(text, 'skill').filter(d => typeof d.name === 'string' && typeof d.body === 'string') : []
function openSkillDraft(d: Record<string, unknown>) {
  try { sessionStorage.setItem('office.skillDraft', JSON.stringify(d)) } catch { /* private mode: the editor opens empty */ }
  navigateTo(`/projects/${props.projectId}/skills/edit`)
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
// the corner chat's switcher: every chat, the current one checked
const threadMenuItems = computed(() => [threadItems.value.length
  ? threadItems.value.map(i => ({ label: i.label, icon: i.value === current.value?.id ? 'i-lucide-check' : undefined, onSelect: () => { threadPick.value = i.value } }))
  : [{ label: t('chat.none'), disabled: true }]])
const threadPick = computed({
  get: () => current.value?.id,
  set: (id?: string) => { const c = conversations.value.find(x => x.id === id); if (c) open(c) }
})

// an editor (skill, template) opened on its chat again: that chat, and the drafts it gave
async function openEditorChat(id: string) {
  try {
    await open({ id } as Conversation)
    emit('history', messages.value.filter(m => m.role === 'assistant').flatMap(m => fencedBlocks(m.content, props.purpose!)))
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

onMounted(() => {
  if (props.purpose === 'automation') openAutomation()
  else if (props.purpose === 'skill' || props.purpose === 'template') { if (typeof route.query.c === 'string') openEditorChat(route.query.c) }
  else if (ownsUrl.value && typeof route.query.draft === 'string' && !tookPrefill) {
    // handed over by the office assistant: a new chat with the message ready to send
    draft.value = route.query.draft
    router.replace({ query: { ...route.query, draft: undefined, c: undefined } })
  } else if (ownsUrl.value && typeof route.query.c === 'string' && !tookPrefill) open({ id: route.query.c } as Conversation, typeof route.query.m === 'string' ? route.query.m : undefined)
  else if (!tookPrefill) openFirst.value = true
})
// the latest chat opens once the list is there
const openFirst = ref(false)
watch([openFirst, convData], () => {
  if (!openFirst.value || !convData.value) return
  openFirst.value = false
  if (!current.value && conversations.value[0] && !tookPrefill) open(conversations.value[0])
})
onBeforeUnmount(() => {
  stopStream()
  stopBackground()
})
</script>

<template>
  <div
    class="flex overflow-hidden rounded-lg border border-(--ui-border)"
    :class="compact || purpose ? 'h-full min-h-0' : 'min-h-[24rem] flex-1 max-sm:-m-3 max-sm:rounded-none max-sm:border-0'"
  >
    <!-- threads -->
    <aside v-if="!single && !compact" class="hidden w-60 shrink-0 flex-col border-e border-(--ui-border) md:flex">
      <ThreadList :loading="convsLoading" v-model:origin="origin" :conversations="conversations" :agents="agents" :current-id="current?.id" :unread="unread.ids.value" :menu="threadMenu" @open="open" @new="newConversation(pick)" />
    </aside>
    <!-- a phone: the chats in a drawer -->
    <USlideover v-if="!single && !compact" v-model:open="threadsOpen" side="left" :title="t('chat.threads')" :ui="{ content: 'max-w-xs', body: 'p-0 sm:p-0 flex flex-col' }">
      <template #body>
        <ThreadList
          :loading="convsLoading" v-model:origin="origin" :conversations="conversations" :agents="agents" :current-id="current?.id" :unread="unread.ids.value" :menu="threadMenu"
          @open="(c) => { threadsOpen = false; open(c) }" @new="threadsOpen = false; newConversation(pick)"
        />
      </template>
    </USlideover>

    <!-- thread -->
    <section class="relative flex min-w-0 flex-1 flex-col">
      <!-- a phone: which chat this is, the drawer of chats, a new one -->
      <div v-if="!single && !compact" class="flex items-center gap-1 border-b border-(--ui-border) p-2 md:hidden">
        <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-panel-left" :aria-label="t('chat.threads')" @click="threadsOpen = true" />
        <p class="min-w-0 flex-1 truncate text-sm font-medium">{{ current?.title || t('chat.newThreadTitle') }}</p>
        <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-square-pen" :aria-label="t('chat.newThread')" @click="newConversation(pick)" />
      </div>
      <!-- compact (the corner chat): back, this chat's title (click to switch), a new one -->
      <div v-if="compact && !single" class="flex items-center gap-1 border-b border-(--ui-border) p-2">
        <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-arrow-left" :aria-label="t('common.close')" @click="emit('back')" />
        <UDropdownMenu :items="threadMenuItems" :content="{ align: 'start' }" :ui="{ content: 'max-h-80 w-72' }" class="min-w-0 flex-1">
          <button type="button" class="flex min-w-0 flex-1 items-center gap-1 rounded-md px-2 py-1 text-left hover:bg-(--ui-bg-elevated)">
            <span class="truncate font-semibold">{{ current?.title || t('chat.newThreadTitle') }}</span>
            <UIcon name="i-lucide-chevron-down" class="size-4 shrink-0 text-(--ui-text-muted)" />
          </button>
        </UDropdownMenu>
        <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-plus" :aria-label="t('chat.newThread')" @click="newConversation()" />
      </div>
      <div v-if="!single && (members.length > 1 || background.length)" class="flex flex-wrap items-center gap-2 border-b border-(--ui-border) px-3 py-1.5 text-xs">
        <span class="text-(--ui-text-muted)">{{ t('chat.members') }}</span>
        <div class="flex -space-x-1.5">
          <span
            v-for="m in members" :key="m.agent_id" class="rounded-full ring-2 ring-(--ui-bg)"
            :title="`${m.agent_name} · ${permOf(m.level).label}${m.context_window ? ` · ${Math.round(m.context_tokens / m.context_window * 100)}% context` : ''}`"
          ><UDropdownMenu :items="agentMenu({ id: m.agent_id })" :content="{ align: 'start' }">
            <button type="button" class="block rounded-full" :aria-label="m.agent_name"><AgentAvatar :agent="agents.find(a => a.id === m.agent_id) ?? { id: m.agent_id, name: m.agent_name }" size="xs" /></button>
          </UDropdownMenu></span>
        </div>
        <span v-for="b in background" :key="b.turn_id" class="flex items-center gap-1 rounded-full bg-(--ui-bg-elevated) py-0.5 ps-2 pe-1 text-(--ui-text-muted)">
          <UIcon name="i-lucide-loader-circle" class="size-3 animate-spin" />{{ t('chat.working', { name: b.agent_name }) }}
          <button type="button" class="rounded-full px-1 hover:text-(--ui-error)" :aria-label="t('chat.stop')" :title="t('chat.stop')" @click="cancelTurn(b.turn_id)">
            <UIcon name="i-lucide-square" class="size-3" />
          </button>
        </span>
      </div>
      <div ref="listEl" class="min-w-0 space-y-4" :class="['flex-1 overflow-y-auto overflow-x-hidden p-4 max-md:px-3', page && 'max-sm:pb-32' /* room for the floating input, open or not: nothing jumps */]">
        <!-- a skill editor's chat, followed from the chat list: back to its editor -->
        <div v-if="!single && current?.purpose === 'skill'" class="flex items-center gap-2 rounded-lg border border-(--ui-border) px-3 py-2 text-sm">
          <UIcon name="i-lucide-sparkles" class="size-4 shrink-0 text-(--ui-primary)" />
          <span class="min-w-0 flex-1 truncate text-(--ui-text-muted)">{{ t('chat.skillChat') }}</span>
          <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-pencil" :label="t('chat.backToSkillEditor')" :to="`/projects/${projectId}/skills/edit?c=${current.id}`" />
        </div>
        <div v-else-if="!single && current?.purpose === 'template'" class="flex items-center gap-2 rounded-lg border border-(--ui-border) px-3 py-2 text-sm">
          <UIcon name="i-lucide-network" class="size-4 shrink-0 text-(--ui-primary)" />
          <span class="min-w-0 flex-1 truncate text-(--ui-text-muted)">{{ t('chat.templateChat') }}</span>
          <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-pencil" :label="t('chat.backToTemplateEditor')" :to="`/templates/new?c=${current.id}`" />
        </div>
        <div v-if="hasOlder" class="text-center">
          <UButton size="xs" color="neutral" variant="soft" icon="i-lucide-arrow-up" :loading="loadingOlder" :label="t('chat.older')" @click="loadOlder" />
        </div>
        <!-- the messages on their way -->
        <div v-if="loadingMsgs && !messages.length" class="space-y-5" aria-busy="true">
          <div v-for="i in 3" :key="i" class="space-y-4">
            <div class="flex justify-end"><USkeleton class="h-9 rounded-2xl" :style="{ width: `${30 + i * 10}%` }" /></div>
            <div class="space-y-2">
              <div class="flex items-center gap-2"><USkeleton class="size-5 rounded-full" /><USkeleton class="h-3 w-24" /></div>
              <USkeleton class="h-3.5 w-11/12" /><USkeleton class="h-3.5 w-4/5" /><USkeleton class="h-3.5 w-2/3" />
            </div>
          </div>
        </div>
        <div v-else-if="!messages.length && !streaming" class="flex h-full flex-col items-center justify-center gap-2 text-center text-(--ui-text-muted)">
          <UIcon name="i-lucide-messages-square" class="size-8" />
          <template v-if="purpose === 'automation'">
            <p class="text-sm">{{ t('chat.askAboutAutomation') }}</p>
            <p class="text-xs">{{ t('chat.automationHint') }}</p>
          </template>
          <template v-else-if="purpose === 'template'">
            <p class="text-sm">{{ t('tplNew.askAI') }}</p>
            <p class="text-xs">{{ t('tplNew.askAIHint') }}</p>
          </template>
          <template v-else-if="purpose === 'skill'">
            <p class="text-sm">{{ t('skill.askAI') }}</p>
            <p class="text-xs">{{ t('skill.askAIHint') }}</p>
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
              <div v-if="m.content" class="min-w-0 whitespace-pre-wrap rounded-2xl rounded-br-sm bg-(--ui-primary) px-3.5 py-2 text-sm text-white"><template v-for="(p, i) in tagged(m.content)" :key="i"><span v-if="p.tag" class="rounded bg-white/20 px-0.5 font-medium">{{ p.text }}</span><template v-else>{{ p.text }}</template></template></div>
            </div>
          </div>
          <div v-else-if="m.role === 'error'" class="flex items-start gap-2 text-sm text-(--ui-error)">
            <UIcon name="i-lucide-circle-alert" class="mt-0.5 size-4 shrink-0" />
            <span>{{ m.content }}</span>
          </div>
          <div v-else :id="`m-${m.id}`" class="group/msg min-w-0 space-y-2 rounded-lg transition" :class="marked === m.id && 'ring-2 ring-primary/60 ring-offset-4 ring-offset-(--ui-bg)'">
            <div class="flex items-center gap-2 text-xs text-(--ui-text-muted)">
              <UDropdownMenu v-if="agents.some(a => a.name === m.author)" :items="agentMenu(agents.find(a => a.name === m.author))" :content="{ align: 'start' }">
                <button type="button" class="block rounded-full" :aria-label="m.author"><AgentAvatar :agent="agents.find(a => a.name === m.author)!" size="xs" /></button>
              </UDropdownMenu>
              <AgentAvatar v-else :agent="{ name: m.author }" size="xs" />
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
            <div class="markdown min-w-0 text-sm" v-html="renderMarkdown(m.content)" />
            <UButton
              v-for="(d, i) in (purpose || current?.purpose === 'skill' || current?.purpose === 'template' ? [] : skillDrafts(m.content))" :key="`sk${i}`"
              icon="i-lucide-sparkles" size="sm" color="neutral" variant="outline" :label="t('chat.openSkillEditor', { name: String(d.name) })"
              @click="openSkillDraft(d)"
            />
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

      <form
        :class="['border-t border-(--ui-border) p-3 max-md:p-2', page && (composeOpen
          ? 'max-sm:absolute max-sm:inset-x-3 max-sm:bottom-3 max-sm:z-10 max-sm:border-0 max-sm:p-0 max-sm:[&>*]:shadow-lg'
          : 'max-sm:hidden')]"
        @submit.prevent="send"
      >
        <PromptInput
          ref="prompt" v-model="draft" v-model:attachments="draftFiles" :project-id="projectId"
          :placeholder="picked ? t('chat.placeholderWithAgent', { agent: picked.name }) : t('chat.placeholderNoAgent')"
          :mentions="single ? [] : agents.map(a => ({ name: a.name, label: permOf(agentLevel(a.permissions)).label, icon: permOf(agentLevel(a.permissions)).icon }))"
          @submit="send"
        >
          <template #actions>
            <ContextMeter :tokens="current?.context_tokens" :window="current?.context_window" />
            <USelect
              v-if="!single && agents.length > 1" v-model="pick" :items="agentItems" size="sm" variant="ghost" class="min-w-0 max-w-56 shrink"
              :icon="permOf(pickedLevel).icon" :title="permOf(pickedLevel).description" :aria-label="t('chat.pickAgent')"
            />
            <EditModePicker v-if="permRank(pickedLevel) >= permRank('propose')" v-model="editMode" class="min-w-0 shrink" />
            <UButton v-if="streaming" size="sm" icon="i-lucide-square" color="neutral" variant="outline" :label="t('chat.stop')" @click="cancel" />
            <UButton v-else size="sm" type="submit" icon="i-lucide-send" class="shrink-0" :disabled="!draft.trim() && !draftFiles.length" />
            <UButton v-if="page" size="sm" color="neutral" variant="ghost" icon="i-lucide-x" :class="'shrink-0 sm:hidden'" :aria-label="t('common.close')" @click="composeOpen = false" />
          </template>
        </PromptInput>
      </form>
      <UButton
        v-if="page && !composeOpen" class="absolute bottom-3 end-3 z-10 rounded-full shadow-lg sm:hidden" size="lg" icon="i-lucide-message-circle"
        :aria-label="t('chat.write')" @click="composeOpen = true; nextTick(() => prompt?.focus?.())"
      />
    </section>
  </div>
</template>

