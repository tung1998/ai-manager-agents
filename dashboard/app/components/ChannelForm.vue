<script setup lang="ts">
// Connecting a Telegram / Discord bot (ADR-048): the steps to make one,
// token, who may write, the reply when no rule takes a message.
const props = defineProps<{ projectId: string, channel?: Channel | null }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ saved: [Channel] }>()
const { t } = useLang()
const toast = useToast()
const editing = computed(() => props.channel ?? null)
const form = reactive({ kind: 'telegram' as Channel['kind'], name: '', token: '', enabled: true, allow: '', refusal: '' })
watch(open, (v) => {
  if (!v) return
  const c = props.channel
  Object.assign(form, c
    ? { kind: c.kind, name: c.name, token: '', enabled: c.enabled, allow: c.allow.join('\n'), refusal: c.refusal }
    : { kind: 'telegram', name: '', token: '', enabled: true, allow: '', refusal: '' })
}, { immediate: true })
const saving = ref(false)
async function save() {
  saving.value = true
  const body: Record<string, unknown> = {
    name: form.name, enabled: form.enabled, allow: form.allow.split(/[\n,]/).map(s => s.trim()).filter(Boolean), refusal: form.refusal
  }
  if (form.token) body.token = form.token
  try {
    const res = editing.value
      ? await $fetch<{ channel: Channel }>(`/api/channels/${editing.value.id}`, { method: 'PATCH', body })
      : await $fetch<{ channel: Channel }>(`/api/projects/${props.projectId}/channels`, { method: 'POST', body: { ...body, kind: form.kind } })
    open.value = false
    emit('saved', res.channel)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    saving.value = false
  }
}
// the steps to make a bot and find the ids, for the kind being added
const guide = computed<{ text: string, link?: { label: string, url: string } }[]>(() => form.kind === 'discord'
  ? [
      { text: t('channels.gd1'), link: { label: 'Discord Developer Portal', url: 'https://discord.com/developers/applications' } },
      { text: t('channels.gd2') },
      { text: t('channels.gd3') },
      { text: t('channels.gd4') }
    ]
  : [
      { text: t('channels.gt1'), link: { label: '@BotFather', url: 'https://t.me/BotFather' } },
      { text: t('channels.gt2') },
      { text: t('channels.gt3'), link: { label: '@userinfobot', url: 'https://t.me/userinfobot' } },
      { text: t('channels.gt4') }
    ])
</script>

<template>
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
          <!-- how to make the bot: numbered steps, open while adding one -->
          <details class="group rounded-lg border border-(--ui-border) bg-(--ui-bg-elevated)/40 p-3" :open="!editing">
            <summary class="flex cursor-pointer list-none items-center gap-2 text-sm font-medium">
              <UIcon name="i-lucide-chevron-right" class="size-4 transition group-open:rotate-90" />
              {{ form.kind === 'discord' ? t('channels.guideDiscord') : t('channels.guideTelegram') }}
            </summary>
            <ol class="mt-3 space-y-2.5">
              <li v-for="(s, i) in guide" :key="i" class="flex gap-2.5 text-sm">
                <span class="flex size-5 shrink-0 items-center justify-center rounded-full bg-primary/15 text-xs font-medium text-primary">{{ i + 1 }}</span>
                <span class="min-w-0">
                  {{ s.text }}
                  <a v-if="s.link" :href="s.link.url" target="_blank" rel="noopener" class="ms-1 inline-flex items-center gap-0.5 text-primary hover:underline">
                    {{ s.link.label }}<UIcon name="i-lucide-external-link" class="size-3" />
                  </a>
                </span>
              </li>
            </ol>
          </details>
          <UFormField :label="t('channels.name')" :help="t('channels.nameHelp')" required>
            <UInput v-model="form.name" name="channel-name" autocomplete="off" class="w-full" :placeholder="t('channels.namePlaceholder')" />
          </UFormField>
          <UFormField :label="t('channels.token')" :help="form.kind === 'discord' ? t('channels.tokenHelpDiscord') : t('channels.tokenHelpTelegram')" :required="!editing">
            <UInput
              v-model="form.token" type="password" name="bot-token" autocomplete="new-password" class="w-full font-mono"
              :placeholder="editing?.has_token ? t('channels.tokenKept') : (form.kind === 'discord' ? 'MTI3…' : '123456789:AAF…')"
            />
          </UFormField>
          <UFormField :label="t('channels.allow')" :help="form.kind === 'discord' ? t('channels.allowHelpDiscord') : t('channels.allowHelpTelegram')" required>
            <UTextarea v-model="form.allow" :rows="2" autoresize class="w-full font-mono text-xs" :placeholder="form.kind === 'discord' ? '123456789012345678' : '123456789'" />
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
</template>
