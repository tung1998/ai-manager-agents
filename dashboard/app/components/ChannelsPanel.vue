<script setup lang="ts">
// Telegram / Discord bots of the project (ADR-048): the connection (token,
// who may write). What a message does is a rule: an automation whose source
// is the channel (ADR-049).
interface Channel {
  id: string, kind: 'telegram' | 'discord', name: string, has_token: boolean, enabled: boolean,
  allow: string[], refusal: string, bot_name: string, last_error: string, last_message_at: string | null
}
const props = defineProps<{ projectId: string }>()
const { t, dateLocale } = useLang()
const toast = useToast()
const { data, refresh } = await useFetch<{ channels: Channel[] }>(() => `/api/projects/${props.projectId}/channels`)
const { data: autos } = useFetch<{ automations: Automation[] }>(() => `/api/projects/${props.projectId}/automations`, { lazy: true })
const rulesOf = (c: Channel) => (autos.value?.automations ?? []).filter(a => isChannelSource(a.source) && a.config.channel_id === c.id)
const newRule = (c: Channel) => `/projects/${props.projectId}/automations/new?channel=${c.id}`
const ruleLink = (a: Automation) => `/projects/${props.projectId}/automations/${a.id}`
const actionIcon: Record<Automation['action'], string> = { chat: 'i-lucide-message-circle-reply', script: 'i-lucide-square-terminal', task: 'i-lucide-list-todo' }

const open = ref(false)
const editing = ref<Channel | null>(null)
const form = reactive({ kind: 'telegram' as Channel['kind'], name: '', token: '', enabled: true, allow: '', refusal: '' })
function edit(c?: Channel) {
  editing.value = c ?? null
  Object.assign(form, c
    ? { kind: c.kind, name: c.name, token: '', enabled: c.enabled, allow: c.allow.join('\n'), refusal: c.refusal }
    : { kind: 'telegram', name: '', token: '', enabled: true, allow: '', refusal: '' })
  open.value = true
}
const saving = ref(false)
async function save() {
  saving.value = true
  const body: Record<string, unknown> = {
    name: form.name, enabled: form.enabled, allow: form.allow.split(/[\n,]/).map(s => s.trim()).filter(Boolean), refusal: form.refusal
  }
  if (form.token) body.token = form.token
  try {
    if (editing.value) await $fetch(`/api/channels/${editing.value.id}`, { method: 'PATCH', body })
    else {
      const res = await $fetch<{ channel: Channel }>(`/api/projects/${props.projectId}/channels`, { method: 'POST', body: { ...body, kind: form.kind } })
      open.value = false
      await navigateTo(newRule(res.channel)) // a bot does nothing until a rule takes its messages
      return
    }
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
      <div v-for="c in data.channels" :key="c.id" class="border-b border-(--ui-border) px-4 py-3 last:border-0">
        <div class="flex flex-wrap items-center gap-3">
          <UIcon :name="c.kind === 'discord' ? 'i-lucide-gamepad-2' : 'i-lucide-send'" class="size-5 shrink-0 text-(--ui-text-muted)" />
          <button type="button" class="min-w-0 flex-1 text-left" @click="edit(c)">
            <span class="block truncate font-medium">{{ c.name }}<span v-if="c.bot_name" class="ms-1 font-normal text-(--ui-text-muted)">@{{ c.bot_name }}</span></span>
            <span class="block truncate text-xs" :class="c.last_error ? 'text-(--ui-error)' : 'text-(--ui-text-muted)'">
              {{ c.last_error || (c.enabled ? t('channels.lastMessage', { when: when(c.last_message_at) }) : t('channels.off')) }}
            </span>
          </button>
          <USwitch :model-value="c.enabled" size="sm" @update:model-value="(v: boolean) => toggle(c, v)" />
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-trash-2" :aria-label="t('common.delete')" @click="remove(c)" />
        </div>
        <!-- what its messages do: the rules, first match wins -->
        <div class="mt-2 ms-8 space-y-1">
          <p class="text-xs font-medium text-(--ui-text-muted)">{{ t('channels.rules') }}</p>
          <NuxtLink
            v-for="(a, i) in rulesOf(c)" :key="a.id" :to="ruleLink(a)"
            class="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-(--ui-bg-elevated)" :class="a.enabled ? '' : 'opacity-60'"
          >
            <span class="w-4 text-xs tabular-nums text-(--ui-text-dimmed)">{{ i + 1 }}</span>
            <UIcon :name="actionIcon[a.action]" class="size-4 shrink-0 text-(--ui-text-muted)" />
            <span class="min-w-0 flex-1 truncate">{{ a.name }}</span>
            <span v-if="a.config.keywords?.length" class="truncate text-xs text-(--ui-text-muted)">{{ a.config.keywords.join(', ') }}</span>
            <UBadge v-if="a.config.scope" :label="t('channels.scoped')" color="neutral" variant="outline" size="sm" />
          </NuxtLink>
          <p v-if="!rulesOf(c).length" class="px-2 text-xs text-(--ui-warning)">{{ t('channels.noRules') }}</p>
          <UButton :to="newRule(c)" size="xs" color="neutral" variant="ghost" icon="i-lucide-plus" :label="t('channels.addRule')" />
        </div>
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
          <UFormField :label="t('channels.allow')" :help="t('channels.allowHelp')">
            <UTextarea v-model="form.allow" :rows="2" autoresize class="w-full font-mono text-xs" />
          </UFormField>
          <UFormField :label="t('channels.refusal')" :help="t('channels.refusalHelp')"><UInput v-model="form.refusal" class="w-full" :placeholder="t('channels.refusalPlaceholder')" /></UFormField>
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
