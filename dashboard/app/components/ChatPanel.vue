<script setup lang="ts">
import type { Patch } from './PatchCard.vue'
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
  cost_usd?: number
}
interface Conversation { id: string, agent_id: string, agent_name: string, title: string, updated_at: string, active_turn?: string }
interface ChatEvent { seq: number, type: 'text' | 'tool' | 'status' | 'patch' | 'done' | 'error', text?: string, tool?: ToolCall, patch?: Patch, message?: Message }

const props = defineProps<{ projectId: string }>()
const toast = useToast()

const { data: agentsData } = await useFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`)
const { data: convData, refresh: refreshConvs } = await useFetch<{ conversations: Conversation[] }>(() => `/api/projects/${props.projectId}/conversations`)
const agents = computed(() => (agentsData.value?.agents ?? []).filter(a => a.tier !== 'worker'))
const conversations = computed(() => convData.value?.conversations ?? [])

const current = ref<Conversation | null>(null)
const messages = ref<Message[]>([])
const draft = ref('')
const draftFiles = ref<Attachment[]>([])
const prompt = ref<{ busy: boolean } | null>(null)

// filled by other tabs (e.g. "Hỏi agent" in Vận hành)
const prefill = useState<{ text: string, files: Attachment[] } | null>('chat-prefill', () => null)
function takePrefill() {
  if (!prefill.value) return
  draft.value = prefill.value.text
  draftFiles.value = prefill.value.files
  prefill.value = null
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

async function open(c: Conversation) {
  stopStream()
  current.value = c
  const res = await $fetch<{ conversation: Conversation, messages: Message[] }>(`/api/conversations/${c.id}`)
  messages.value = res.messages
  current.value = res.conversation
  if (res.conversation.active_turn) follow(res.conversation.active_turn)
  scrollDown()
}

async function newConversation(agentId = '') {
  try {
    const res = await $fetch<{ conversation: Conversation }>(`/api/projects/${props.projectId}/conversations`, { method: 'POST', body: { agent_id: agentId } })
    await refreshConvs()
    await open(res.conversation)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

async function send() {
  const text = draft.value.trim()
  if ((!text && !draftFiles.value.length) || streaming.value || prompt.value?.busy) return
  if (!current.value) await newConversation()
  if (!current.value) return
  try {
    const res = await $fetch<{ turn_id: string, message: Message }>(`/api/conversations/${current.value.id}/messages`, { method: 'POST', body: { text, attachments: draftFiles.value.map(a => a.id) } })
    draft.value = ''
    draftFiles.value = []
    messages.value.push(res.message)
    follow(res.turn_id)
    scrollDown()
  } catch (e) {
    const d = (e as { data?: { code?: string, error?: string } }).data
    if (d?.code === 'budget') {
      toast.add({ title: 'Đã chạm trần chi phí', description: d.error, color: 'warning', actions: [{ label: 'Xem Chi phí', onClick: () => { navigateTo('/costs') } }] })
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
        if (ev.message) messages.value.push(ev.message)
        finishStream()
        refreshConvs()
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
  if (!confirm('Xóa cuộc trò chuyện này?')) return
  await $fetch(`/api/conversations/${c.id}`, { method: 'DELETE' })
  if (current.value?.id === c.id) {
    current.value = null
    messages.value = []
  }
  refreshConvs()
}

function onPatchUpdated(msg: Message, p: Patch) {
  const i = msg.patches.findIndex(x => x.id === p.id)
  if (i >= 0) msg.patches[i] = p
}

const agentMenu = computed(() => [agents.value.map(a => ({ label: a.name, description: a.role, onSelect: () => newConversation(a.id) }))])
const when = (d: string) => new Date(d).toLocaleString('vi-VN', { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })

onMounted(() => { if (conversations.value[0]) open(conversations.value[0]) })
onBeforeUnmount(stopStream)
</script>

<template>
  <div class="flex h-[calc(100vh-13rem)] min-h-[28rem] overflow-hidden rounded-lg border border-(--ui-border)">
    <!-- threads -->
    <aside class="hidden w-60 shrink-0 flex-col border-e border-(--ui-border) md:flex">
      <div class="flex items-center gap-1 border-b border-(--ui-border) p-2">
        <UButton icon="i-lucide-square-pen" label="Trò chuyện mới" size="sm" color="neutral" variant="ghost" class="flex-1 justify-start" @click="newConversation()" />
        <UDropdownMenu v-if="agents.length > 1" :items="agentMenu">
          <UButton icon="i-lucide-chevron-down" size="sm" color="neutral" variant="ghost" title="Chọn agent" />
        </UDropdownMenu>
      </div>
      <div class="flex-1 overflow-y-auto p-1">
        <p v-if="!conversations.length" class="p-3 text-xs text-(--ui-text-muted)">Chưa có cuộc trò chuyện.</p>
        <div
          v-for="c in conversations" :key="c.id"
          class="group flex cursor-pointer items-start gap-1 rounded-md px-2 py-1.5 text-sm"
          :class="current?.id === c.id ? 'bg-(--ui-bg-accented)' : 'hover:bg-(--ui-bg-muted)'"
          @click="open(c)"
        >
          <div class="min-w-0 flex-1">
            <p class="truncate">{{ c.title || 'Cuộc trò chuyện mới' }}</p>
            <p class="truncate text-xs text-(--ui-text-muted)">{{ c.agent_name }} · {{ when(c.updated_at) }}</p>
          </div>
          <UIcon v-if="c.active_turn" name="i-lucide-loader-circle" class="mt-1 size-3.5 animate-spin text-(--ui-text-muted)" />
          <button type="button" class="invisible mt-0.5 text-(--ui-text-dimmed) group-hover:visible" title="Xóa" @click.stop="remove(c)">
            <UIcon name="i-lucide-trash-2" class="size-3.5" />
          </button>
        </div>
      </div>
    </aside>

    <!-- thread -->
    <section class="flex min-w-0 flex-1 flex-col">
      <div ref="listEl" class="flex-1 space-y-4 overflow-y-auto p-4">
        <div v-if="!messages.length && !streaming" class="flex h-full flex-col items-center justify-center gap-2 text-center text-(--ui-text-muted)">
          <UIcon name="i-lucide-messages-square" class="size-8" />
          <p class="text-sm">Hỏi agent về project: giải thích code, tìm lỗi, đề xuất sửa…</p>
          <p class="text-xs">Agent chỉ đọc; mọi thay đổi code đều cần bạn duyệt.</p>
        </div>

        <template v-for="m in messages" :key="m.id">
          <div v-if="m.role === 'user'" class="flex flex-col items-end gap-1.5">
            <AttachmentList :items="m.attachments ?? []" align="end" />
            <div v-if="m.content" class="max-w-[80%] whitespace-pre-wrap rounded-2xl rounded-br-sm bg-(--ui-primary) px-3.5 py-2 text-sm text-white">{{ m.content }}</div>
          </div>
          <div v-else-if="m.role === 'error'" class="flex items-start gap-2 text-sm text-(--ui-error)">
            <UIcon name="i-lucide-circle-alert" class="mt-0.5 size-4 shrink-0" />
            <span>{{ m.content }}</span>
          </div>
          <div v-else class="space-y-2">
            <div class="flex items-center gap-2 text-xs text-(--ui-text-muted)">
              <UIcon name="i-lucide-bot" class="size-4 text-primary" />
              <span class="font-medium">{{ m.author }}</span>
              <span>{{ when(m.created_at) }}</span>
              <span v-if="m.cost_usd">· ${{ m.cost_usd.toFixed(3) }}</span>
            </div>
            <details v-if="m.tools.length" class="text-xs text-(--ui-text-muted)">
              <summary class="cursor-pointer">Đã dùng {{ m.tools.length }} công cụ</summary>
              <ul class="mt-1 space-y-0.5 ps-4">
                <li v-for="(t, i) in m.tools" :key="i" :class="t.error ? 'text-(--ui-error)' : ''">{{ t.summary }}</li>
              </ul>
            </details>
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div class="markdown text-sm" v-html="renderMarkdown(m.content)" />
            <PatchCard v-for="p in m.patches" :key="p.id" :patch="p" @updated="(np: Patch) => onPatchUpdated(m, np)" />
          </div>
        </template>

        <div v-if="streaming" class="space-y-2">
          <div class="flex items-center gap-2 text-xs text-(--ui-text-muted)">
            <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin text-primary" />
            <span>{{ liveStatus || 'Đang trả lời…' }}</span>
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
          :placeholder="current ? `Nhắn ${current.agent_name}… (Enter gửi, Shift+Enter xuống dòng)` : 'Nhắn agent… (Enter gửi)'"
          @submit="send"
        >
          <template #actions>
            <UButton v-if="streaming" size="sm" icon="i-lucide-square" color="neutral" variant="outline" label="Dừng" @click="cancel" />
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
