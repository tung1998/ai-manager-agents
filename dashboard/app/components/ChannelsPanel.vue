<script setup lang="ts">
// Telegram / Discord bots of the project (ADR-048): outside people message
// the bot, an agent answers in the same chat; scope filter and allow list.
interface Channel {
  id: string, kind: 'telegram' | 'discord', name: string, has_token: boolean, agent_id: string, mode: PermLevel, enabled: boolean,
  allow: string[], scope: string, filter_enabled: boolean, refusal: string, bot_name: string, last_error: string, last_message_at: string | null
}
const props = defineProps<{ projectId: string }>()
const { t, dateLocale } = useLang()
const toast = useToast()
const { data, refresh } = await useFetch<{ channels: Channel[] }>(() => `/api/projects/${props.projectId}/channels`)
const { data: agentsData } = useFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
const LEAD = '__lead'
const agentItems = computed(() => [{ label: t('channels.agentLead'), value: LEAD }, ...(agentsData.value?.agents ?? []).map(a => ({ label: a.name, value: a.id }))])
const modeItems = computed(() => permLevels.map(l => ({ label: l.label, value: l.level })))

const open = ref(false)
const editing = ref<Channel | null>(null)
const form = reactive({ kind: 'telegram' as Channel['kind'], name: '', token: '', agent: LEAD, mode: 'read' as PermLevel, enabled: true, allow: '', scope: '', filter_enabled: false, refusal: '' })
function edit(c?: Channel) {
  editing.value = c ?? null
  Object.assign(form, c
    ? { kind: c.kind, name: c.name, token: '', agent: c.agent_id || LEAD, mode: c.mode, enabled: c.enabled, allow: c.allow.join('\n'), scope: c.scope, filter_enabled: c.filter_enabled, refusal: c.refusal }
    : { kind: 'telegram', name: '', token: '', agent: LEAD, mode: 'read', enabled: true, allow: '', scope: '', filter_enabled: false, refusal: '' })
  open.value = true
}
const saving = ref(false)
async function save() {
  saving.value = true
  const body: Record<string, unknown> = {
    name: form.name, agent_id: form.agent === LEAD ? '' : form.agent, mode: form.mode, enabled: form.enabled,
    allow: form.allow.split(/[\n,]/).map(s => s.trim()).filter(Boolean), scope: form.scope, filter_enabled: form.filter_enabled, refusal: form.refusal
  }
  if (form.token) body.token = form.token
  try {
    if (editing.value) await $fetch(`/api/channels/${editing.value.id}`, { method: 'PATCH', body })
    else await $fetch(`/api/projects/${props.projectId}/channels`, { method: 'POST', body: { ...body, kind: form.kind } })
    open.value = false
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    saving.value = false
  }
}
async function toggle(c: Channel, enabled: boolean) {
  await $fetch(`/api/channels/${c.id}`, { method: 'PATCH', body: { enabled } }).catch(e => toast.add({ title: apiError(e), color: 'error' }))
  await refresh()
}
async function remove(c: Channel) {
  if (!confirm(t('channels.deleteConfirm', { name: c.name }))) return
  await $fetch(`/api/channels/${c.id}`, { method: 'DELETE' }).catch(e => toast.add({ title: apiError(e), color: 'error' }))
  await refresh()
}
const when = (d: string | null) => d ? new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' }) : t('channels.noMessage')
// the status every few seconds while one is connecting
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { timer = setInterval(() => refresh(), 10000) })
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="max-w-4xl space-y-3">
    <div class="flex items-center justify-between gap-2">
      <p class="text-sm text-(--ui-text-muted)">{{ t('channels.intro') }}</p>
      <UButton icon="i-lucide-plus" size="sm" :label="t('channels.new')" @click="edit()" />
    </div>
    <UCard v-if="data?.channels.length" :ui="{ body: 'p-0 sm:p-0' }">
      <div v-for="c in data.channels" :key="c.id" class="flex flex-wrap items-center gap-3 border-b border-(--ui-border) px-4 py-3 last:border-0">
        <UIcon :name="c.kind === 'discord' ? 'i-lucide-gamepad-2' : 'i-lucide-send'" class="size-5 shrink-0 text-(--ui-text-muted)" />
        <button type="button" class="min-w-0 flex-1 text-left" @click="edit(c)">
          <span class="block truncate font-medium">{{ c.name }}<span v-if="c.bot_name" class="ms-1 font-normal text-(--ui-text-muted)">@{{ c.bot_name }}</span></span>
          <span class="block truncate text-xs" :class="c.last_error ? 'text-(--ui-error)' : 'text-(--ui-text-muted)'">
            {{ c.last_error || (c.enabled ? t('channels.lastMessage', { when: when(c.last_message_at) }) : t('channels.off')) }}
            <template v-if="c.filter_enabled"> · {{ t('channels.filtered') }}</template>
          </span>
        </button>
        <UBadge :label="permOf(c.mode).label" color="neutral" variant="outline" size="sm" />
        <USwitch :model-value="c.enabled" size="sm" @update:model-value="(v: boolean) => toggle(c, v)" />
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-trash-2" :aria-label="t('common.delete')" @click="remove(c)" />
      </div>
    </UCard>
    <p v-else class="text-sm text-(--ui-text-muted)">{{ t('channels.empty') }}</p>

    <UModal v-model:open="open" :title="editing ? t('channels.edit') : t('channels.new')" :ui="{ content: 'max-w-xl' }">
      <template #body>
        <div class="space-y-3">
          <div v-if="!editing" class="flex rounded-lg bg-(--ui-bg-elevated) p-0.5 text-sm">
            <button
              v-for="k in (['telegram', 'discord'] as const)" :key="k" type="button" class="flex flex-1 items-center justify-center gap-1.5 rounded-md py-1"
              :class="form.kind === k ? 'bg-(--ui-bg) font-medium shadow-sm' : 'text-(--ui-text-muted)'" @click="form.kind = k"
            >
              <UIcon :name="k === 'discord' ? 'i-lucide-gamepad-2' : 'i-lucide-send'" class="size-4" />{{ k === 'discord' ? 'Discord' : 'Telegram' }}
            </button>
          </div>
          <p class="text-xs text-(--ui-text-muted)">{{ form.kind === 'discord' ? t('channels.howDiscord') : t('channels.howTelegram') }}</p>
          <UFormField :label="t('channels.name')" required><UInput v-model="form.name" class="w-full" /></UFormField>
          <UFormField :label="t('channels.token')" :required="!editing">
            <UInput v-model="form.token" type="password" class="w-full font-mono" :placeholder="editing?.has_token ? t('channels.tokenKept') : ''" />
          </UFormField>
          <div class="grid gap-3 sm:grid-cols-2">
            <UFormField :label="t('channels.agent')"><USelect v-model="form.agent" :items="agentItems" class="w-full" /></UFormField>
            <UFormField :label="t('channels.mode')" :help="t('channels.modeHelp')"><USelect v-model="form.mode" :items="modeItems" class="w-full" /></UFormField>
          </div>
          <UFormField :label="t('channels.allow')" :help="t('channels.allowHelp')">
            <UTextarea v-model="form.allow" :rows="2" autoresize class="w-full font-mono text-xs" />
          </UFormField>
          <UFormField :label="t('channels.scope')" :help="t('channels.scopeHelp')">
            <UTextarea v-model="form.scope" :rows="2" autoresize class="w-full" />
          </UFormField>
          <USwitch v-model="form.filter_enabled" :label="t('channels.filter')" :description="t('channels.filterHelp')" />
          <UFormField v-if="form.filter_enabled" :label="t('channels.refusal')"><UInput v-model="form.refusal" class="w-full" :placeholder="t('channels.refusalPlaceholder')" /></UFormField>
          <USwitch v-model="form.enabled" :label="t('channels.enabled')" />
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" :label="t('common.cancel')" @click="open = false" />
          <UButton :label="t('common.save')" :loading="saving" @click="save" />
        </div>
      </template>
    </UModal>
  </div>
</template>
