<script setup lang="ts">
// The bots connected to a project, on top of its automations (ADR-049): a
// bot is a source; its rules are the automations below.
const props = defineProps<{ projectId: string }>()
const { t, dateLocale } = useLang()
const toast = useToast()
const { data, refresh } = await useFetch<{ channels: Channel[] }>(() => `/api/projects/${props.projectId}/channels`)
const channels = computed(() => data.value?.channels ?? [])
const open = ref(false)
const editing = ref<Channel | null>(null)
function edit(c?: Channel) {
  editing.value = c ?? null
  open.value = true
}
async function saved(c: Channel) {
  const added = !editing.value
  await refresh()
  if (added) await navigateTo(`/projects/${props.projectId}/automations/new?channel=${c.id}`) // a bot does nothing until a rule takes its messages
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
const status = (c: Channel) => c.last_error ? { color: 'text-(--ui-error)', dot: 'bg-(--ui-error)', text: c.last_error }
  : !c.enabled ? { color: 'text-(--ui-text-muted)', dot: 'bg-(--ui-text-dimmed)', text: t('channels.off') }
    : c.bot_name ? { color: 'text-(--ui-text-muted)', dot: 'bg-(--ui-success)', text: t('channels.lastMessage', { when: when(c.last_message_at) }) }
      : { color: 'text-(--ui-text-muted)', dot: 'bg-(--ui-warning)', text: t('channels.connecting') }
// the status every few seconds while one is connecting
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { timer = setInterval(() => refresh(), 10000) })
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <section class="rounded-xl border border-(--ui-border) p-3">
    <div class="mb-2 flex items-center justify-between gap-2">
      <p class="flex items-center gap-1.5 text-sm font-semibold"><UIcon name="i-lucide-bot" class="size-4 text-(--ui-text-muted)" />{{ t('channels.connected') }}</p>
      <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-plus" :label="t('channels.connect')" @click="edit()" />
    </div>
    <p v-if="!channels.length" class="text-xs text-(--ui-text-muted)">{{ t('channels.empty') }}</p>
    <div v-for="c in channels" :key="c.id" class="flex items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-(--ui-bg-elevated)/60">
      <UIcon :name="c.kind === 'discord' ? 'i-lucide-gamepad-2' : 'i-lucide-send'" class="size-4 shrink-0 text-(--ui-text-muted)" />
      <button type="button" class="min-w-0 flex-1 text-left" @click="edit(c)">
        <span class="block truncate text-sm font-medium">{{ c.name }}<span v-if="c.bot_name" class="ms-1 font-normal text-(--ui-text-muted)">@{{ c.bot_name }}</span></span>
        <span class="flex items-center gap-1.5 truncate text-xs" :class="status(c).color">
          <span class="size-1.5 shrink-0 rounded-full" :class="status(c).dot" />{{ status(c).text }}
        </span>
      </button>
      <USwitch :model-value="c.enabled" size="sm" @update:model-value="(v: boolean) => toggle(c, v)" />
      <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-trash-2" :aria-label="t('common.delete')" @click="remove(c)" />
    </div>
    <ChannelForm v-model:open="open" :project-id="projectId" :channel="editing" @saved="saved" />
  </section>
</template>
