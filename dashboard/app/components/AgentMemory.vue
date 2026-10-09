<script setup lang="ts">
// An agent's long-term notes (ADR-068): put at the top of every new
// conversation. People see them; admins add, edit, remove, compact, put back
// an earlier version, and decide whether the agent's own notes need approval.
// Two tiers (ADR-134): core notes are always loaded; a topic's are not, only
// its summary line (the agent reads them with recall).
interface Note { id: string, text: string, topic: string, summary: string, version?: string, source: 'person' | 'agent' | 'compact', created_by: string, updated_at: string }
interface Rev { id: string, reason: string, count: number, items: string[], created_at: string }
interface Group { topic: string, summary: string, size: number, notes: Note[] }
const props = defineProps<{ projectId: string, agentId: string }>()
const { t, dateLocale } = useLang()
const { isAdmin } = useAuth()
const toast = useToast()
const saveError = useSaveError()
const base = computed(() => `/api/projects/${props.projectId}/agents/${props.agentId}/memories`)
const { data, refresh, error: loadError } = useLiveFetch<{ items: Note[], revisions: Rev[], auto: boolean, size: number, limit: number, topic_limit: number }>(base, { lazy: true })

// core first, then the topics, the most recently written first
const groups = computed<Group[]>(() => {
  const by = new Map<string, Group>()
  for (const n of data.value?.items ?? []) {
    const g = by.get(n.topic) ?? { topic: n.topic, summary: '', size: 0, notes: [] }
    by.delete(n.topic)
    by.set(n.topic, g) // moved to the end: the newest topic last
    g.notes.push(n)
    g.size += [...n.text].length
    if (n.summary) g.summary = n.summary
  }
  const core = by.get('')
  by.delete('')
  return [...(core ? [core] : []), ...[...by.values()].reverse()]
})
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
const sourceLabel = (s: Note['source']) => ({ person: t('mem.fromPerson'), agent: t('mem.fromAgent'), compact: t('mem.fromCompact') })[s]
const sourceIcon = (s: Note['source']) => ({ person: 'i-lucide-user', agent: 'i-lucide-bot', compact: 'i-lucide-shrink' })[s]

async function run(fn: () => Promise<unknown>, ok?: string) {
  try {
    await fn()
    if (ok) toast.add({ title: ok, color: 'success' })
    await refresh()
  } catch (e) {
    saveError(e, refresh)
  }
}

const draft = ref('')
const draftTopic = ref('')
const draftSummary = ref('')
const saving = ref(false)
const add = () => {
  if (saving.value) return
  saving.value = true
  run(async () => {
    await $fetch(base.value, { method: 'POST', body: { text: draft.value, topic: draftTopic.value, summary: draftSummary.value } })
    draft.value = ''
  }).finally(() => { saving.value = false })
}
const editing = ref<string | null>(null)
const editText = ref('')
const editTopic = ref('')
const editSummary = ref('')
let editFrom: string | undefined // the note as it was opened (409 when changed since)
function startEdit(n: Note) {
  editing.value = n.id
  editText.value = n.text
  editTopic.value = n.topic
  editSummary.value = n.summary
  editFrom = n.version
}
const saveEdit = () => {
  if (saving.value) return
  saving.value = true
  run(async () => {
    await $fetch(`/api/memories/${editing.value}`, { method: 'PATCH', body: { text: editText.value, topic: editTopic.value, summary: editSummary.value, version: editFrom } })
    editing.value = null
  }).finally(() => { saving.value = false })
}
const remove = (n: Note) => { if (confirm(t('mem.deleteConfirm'))) run(() => $fetch(`/api/memories/${n.id}`, { method: 'DELETE' })) }
const compacting = ref<string | null>(null) // the topic being compacted ('' = core)
async function compact(topic = '') {
  compacting.value = topic
  await run(() => $fetch(`${base.value}/compact`, { method: 'POST', query: topic ? { topic } : undefined }), t('mem.compacted'))
  compacting.value = null
}
const setAuto = (v: boolean) => run(() => $fetch(`/api/projects/${props.projectId}/memory-settings`, { method: 'PUT', body: { auto: v } }))
const restore = (r: Rev) => { if (confirm(t('mem.restoreConfirm', { n: r.count }))) run(() => $fetch(`/api/memory-revisions/${r.id}/restore`, { method: 'POST' }), t('mem.restored')) }
const revsOpen = ref(false)
const shownRev = ref<string | null>(null)
</script>

<template>
  <div class="space-y-3">
    <UCard :ui="{ body: 'space-y-3 sm:p-4' }">
      <div class="flex flex-wrap items-center gap-2">
        <p class="font-medium">{{ t('mem.title') }}</p>
        <UTooltip :text="t('mem.help')"><UIcon name="i-lucide-info" class="size-4 text-(--ui-text-muted)" /></UTooltip>
        <div class="ms-auto flex items-center gap-2">
          <label v-if="isAdmin && data" class="flex items-center gap-2 text-xs text-(--ui-text-muted)">
            <USwitch :model-value="data.auto" size="sm" @update:model-value="setAuto" />{{ t('mem.auto') }}
          </label>
        </div>
      </div>

      <UAlert v-if="loadError" color="error" variant="subtle" icon="i-lucide-triangle-alert" :title="t('common.loadError')" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
      <LoadingRows v-else-if="!data" :n="3" :icon="false" />
      <p v-else-if="!data.items.length" class="rounded-lg border border-dashed border-(--ui-border) p-4 text-center text-sm text-(--ui-text-muted)">{{ t('mem.empty') }}</p>
      <template v-else>
      <section v-for="g in groups" :key="g.topic" class="space-y-1.5">
        <div class="flex items-center gap-2 text-sm">
          <UIcon :name="g.topic ? 'i-lucide-folder' : 'i-lucide-pin'" class="size-4 shrink-0 text-(--ui-text-muted)" />
          <span class="font-medium">{{ g.topic || t('mem.core') }}</span>
          <UTooltip :text="g.topic ? t('mem.topicsHelp') : t('mem.coreHelp')"><UIcon name="i-lucide-info" class="size-3.5 text-(--ui-text-muted)" /></UTooltip>
          <span v-if="g.summary" class="min-w-0 truncate text-xs text-(--ui-text-muted)">{{ g.summary }}</span>
          <span class="ms-auto shrink-0 text-xs text-(--ui-text-muted) tabular-nums">{{ t('mem.size', { n: g.size, max: g.topic ? data.topic_limit : data.limit }) }}</span>
          <UButton v-if="isAdmin" size="xs" color="neutral" variant="ghost" icon="i-lucide-shrink" :loading="compacting === g.topic" :aria-label="t('mem.compact')" :title="t('mem.compact')" @click="compact(g.topic)" />
        </div>
        <UProgress v-if="!g.topic && data.limit" :model-value="Math.min(100, g.size / data.limit * 100)" size="xs" :color="g.size > data.limit * 0.9 ? 'warning' : 'primary'" />
        <ul class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
          <li v-for="n in g.notes" :key="n.id" class="group flex items-start gap-2 px-3 py-2 text-sm">
            <UIcon :name="sourceIcon(n.source)" class="mt-0.5 size-4 shrink-0 text-(--ui-text-muted)" :title="sourceLabel(n.source)" />
            <div class="min-w-0 flex-1">
              <template v-if="editing === n.id">
                <UTextarea v-model="editText" :rows="2" autoresize class="w-full" />
                <div class="mt-1 flex flex-wrap gap-1">
                  <UInput v-model="editTopic" size="xs" class="w-40" :placeholder="t('mem.topicPlaceholder')" :aria-label="t('mem.topic')" />
                  <UInput v-if="editTopic.trim()" v-model="editSummary" size="xs" class="min-w-0 flex-1" :placeholder="t('mem.summary')" :aria-label="t('mem.summary')" />
                  <div class="ms-auto flex gap-1">
                    <UButton size="xs" color="neutral" variant="ghost" :label="t('common.cancel')" @click="editing = null" />
                    <UButton size="xs" :label="t('common.save')" :loading="saving" :disabled="saving" @click="saveEdit" />
                  </div>
                </div>
              </template>
              <template v-else>
                <p class="whitespace-pre-wrap break-words">{{ n.text }}</p>
                <p class="text-xs text-(--ui-text-muted)">{{ sourceLabel(n.source) }}<template v-if="n.created_by"> · {{ n.created_by.replace(/^human:/, '') }}</template> · {{ when(n.updated_at) }}</p>
              </template>
            </div>
            <div v-if="isAdmin && editing !== n.id" class="flex shrink-0 gap-0.5 md:invisible md:group-hover:visible">
              <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-pencil" :aria-label="t('common.edit')" @click="startEdit(n)" />
              <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-trash-2" class="hover:text-(--ui-error)" :aria-label="t('chat.delete')" @click="remove(n)" />
            </div>
          </li>
        </ul>
      </section>
      </template>
      <form v-if="isAdmin && data" class="flex flex-wrap gap-2" @submit.prevent="add">
        <UInput v-model="draft" class="min-w-0 flex-1 basis-60" :placeholder="t('mem.addPlaceholder')" />
        <UInput v-model="draftTopic" class="w-40" :placeholder="t('mem.topicPlaceholder')" :aria-label="t('mem.topic')" />
        <UInput v-if="draftTopic.trim()" v-model="draftSummary" class="min-w-0 flex-1 basis-40" :placeholder="t('mem.summary')" :aria-label="t('mem.summary')" />
        <UButton type="submit" icon="i-lucide-plus" :label="t('mem.add')" :loading="saving" :disabled="!draft.trim() || saving" />
      </form>
    </UCard>

    <UCard v-if="data?.revisions.length" :ui="{ body: 'space-y-2 sm:p-4' }">
      <button type="button" class="flex w-full items-center gap-2 text-left text-sm font-medium" @click="revsOpen = !revsOpen">
        <UIcon :name="revsOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-4" />{{ t('mem.revisions', { n: data.revisions.length }) }}
      </button>
      <ul v-if="revsOpen" class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
        <li v-for="r in data.revisions" :key="r.id" class="px-3 py-2 text-sm">
          <div class="flex items-center gap-2">
            <button type="button" class="min-w-0 flex-1 truncate text-left" @click="shownRev = shownRev === r.id ? null : r.id">
              {{ when(r.created_at) }} · {{ t('mem.revCount', { n: r.count }) }} <span class="text-(--ui-text-muted)">· {{ r.reason }}</span>
            </button>
            <UButton v-if="isAdmin" size="xs" color="neutral" variant="outline" icon="i-lucide-rotate-ccw" :label="t('mem.restore')" @click="restore(r)" />
          </div>
          <ul v-if="shownRev === r.id" class="mt-1 list-disc ps-5 text-xs text-(--ui-text-muted)">
            <li v-for="(x, i) in r.items" :key="i">{{ x }}</li>
          </ul>
        </li>
      </ul>
    </UCard>
  </div>
</template>
