<script setup lang="ts">
// Create or edit an automation in four steps: trigger, action, content,
// limits. A new webhook's secret is shown once, in a modal only a button closes.
const props = defineProps<{ projectId: string, automation: Automation | null }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ saved: [Automation] }>()
const toast = useToast()
const { t, dateLocale } = useLang()

const { data: agentsData } = useFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
const agentOptions = computed(() => [{ label: t('auto.agentDefault'), value: '' }, ...(agentsData.value?.agents ?? []).map(a => ({ label: a.name, value: a.id }))])
const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone

const empty = (): Omit<Automation, 'id' | 'project_id' | 'failures' | 'disabled_code' | 'disabled_reason' | 'last_run_at' | 'next_run_at' | 'last_job' | 'created_at'> => ({
  name: '', enabled: true, source: 'schedule', config: { every_minutes: 0, cron: '0 8 * * 1-5', timezone: browserTz, auth: 'bearer', auth_name: '' },
  action: 'task', agent_id: '', prompt: '', edit_mode: 'worktree', keep_context: false,
  limits: { max_runs_per_hour: 0, daily_cost_usd: 0, disable_after_failures: 5, debounce_seconds: 0, debounce_key: '', debounce_max_seconds: 0 }
})
const form = reactive(empty())
watch(open, (v) => {
  if (!v) return
  const a = props.automation
  Object.assign(form, empty(), a ? JSON.parse(JSON.stringify(automationBody(a))) : {})
  form.config = { ...empty().config, ...(a?.config ?? {}) }
  form.limits = { ...empty().limits, ...(a?.limits ?? {}) }
})

const presets = computed(() => [
  { label: t('auto.presetEvery5'), every: 5, cron: '' },
  { label: t('auto.presetHourly'), every: 0, cron: '0 * * * *' },
  { label: t('auto.presetWeekday8'), every: 0, cron: '0 8 * * 1-5' },
  { label: t('auto.presetDaily8'), every: 0, cron: '0 8 * * *' }
])
function usePreset(p: { every: number, cron: string }) {
  form.config.every_minutes = p.every
  form.config.cron = p.cron
}

// next runs, asked from the server (same parser as the scheduler)
const preview = ref<{ next: string[], error?: string }>({ next: [] })
let timer: ReturnType<typeof setTimeout> | undefined
watch(() => [form.source, form.config.every_minutes, form.config.cron, form.config.timezone], () => {
  clearTimeout(timer)
  if (form.source !== 'schedule') return
  timer = setTimeout(async () => {
    const q = new URLSearchParams({ every: String(form.config.every_minutes || 0), cron: form.config.cron ?? '', tz: form.config.timezone ?? '' })
    try {
      preview.value = await $fetch(`/api/automations/preview-schedule?${q}`)
    } catch { preview.value = { next: [] } }
  }, 400)
}, { immediate: true })
const fmt = (d: string) => new Date(d).toLocaleString(dateLocale.value, { weekday: 'short', day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })

const placeholders = ['{{payload}}', '{{payload.x}}', '{{now}}', '{{today}}', '{{yesterday}}', '{{source}}', '{{automation}}']
const promptEl = ref<{ textareaRef?: HTMLTextAreaElement } | null>(null)
function insert(p: string) {
  const el = promptEl.value?.textareaRef
  const at = el?.selectionStart ?? form.prompt.length
  form.prompt = form.prompt.slice(0, at) + p + form.prompt.slice(at)
}

const saving = ref(false)
const secret = ref<{ url: string, secret: string } | null>(null)
async function save() {
  saving.value = true
  try {
    const body = automationBody(form)
    const res = props.automation
      ? await $fetch<{ automation: Automation, secret?: string }>(`/api/automations/${props.automation.id}`, { method: 'PATCH', body })
      : await $fetch<{ automation: Automation, secret?: string }>(`/api/projects/${props.projectId}/automations`, { method: 'POST', body })
    toast.add({ title: t('auto.saved'), color: 'success' })
    emit('saved', res.automation)
    open.value = false
    if (res.secret) secret.value = { url: `${location.origin}${res.automation.webhook_url}`, secret: res.secret }
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <USlideover v-model:open="open" :title="automation ? automation.name : t('auto.new')" :ui="{ content: 'max-w-xl' }">
    <template #body>
      <form id="automation-form" class="space-y-5" @submit.prevent="save">
        <UFormField :label="t('auto.name')" required>
          <UInput v-model="form.name" class="w-full" :placeholder="t('auto.namePlaceholder')" />
        </UFormField>

        <!-- 1. trigger -->
        <section class="space-y-3">
          <p class="text-sm font-medium">{{ t('auto.stepSource') }}</p>
          <div class="grid grid-cols-2 gap-2">
            <button
              v-for="s in (['schedule', 'webhook'] as const)" :key="s" type="button"
              class="flex items-center gap-2 rounded-lg border px-3 py-2 text-sm font-medium"
              :class="form.source === s ? 'border-primary bg-primary/5' : 'border-(--ui-border)'"
              @click="form.source = s"
            >
              <UIcon :name="s === 'schedule' ? 'i-lucide-alarm-clock' : 'i-lucide-webhook'" class="size-4" />
              {{ s === 'schedule' ? t('auto.sourceSchedule') : t('auto.sourceWebhook') }}
            </button>
          </div>
          <template v-if="form.source === 'schedule'">
            <div class="flex flex-wrap gap-1.5">
              <UButton v-for="p in presets" :key="p.label" size="xs" color="neutral" variant="outline" :label="p.label" @click="usePreset(p)" />
            </div>
            <div class="grid gap-3 sm:grid-cols-3">
              <UFormField :label="t('auto.everyMinutes')"><UInputNumber v-model="form.config.every_minutes" :min="0" class="w-full" /></UFormField>
              <UFormField :label="t('auto.cron')" :hint="t('auto.cronHelp')" class="sm:col-span-2"><UInput v-model="form.config.cron" class="w-full font-mono" placeholder="0 8 * * 1-5" /></UFormField>
            </div>
            <UFormField :label="t('auto.timezone')"><UInput v-model="form.config.timezone" class="w-full font-mono" /></UFormField>
            <div class="rounded-md bg-(--ui-bg-elevated)/60 px-3 py-2 text-xs">
              <p class="mb-1 font-medium text-(--ui-text-muted)">{{ t('auto.nextRuns') }}</p>
              <p v-if="preview.error" class="text-(--ui-error)">{{ preview.error }}</p>
              <p v-for="n in preview.next" v-else :key="n" class="tabular-nums">{{ fmt(n) }}</p>
            </div>
          </template>
          <template v-else>
            <div class="grid gap-3 sm:grid-cols-2">
              <UFormField :label="t('auto.auth')">
                <USelect v-model="form.config.auth" class="w-full" :items="[{ label: t('auto.authBearer'), value: 'bearer' }, { label: t('auto.authHeader'), value: 'header' }, { label: t('auto.authQuery'), value: 'query' }]" />
              </UFormField>
              <UFormField v-if="form.config.auth !== 'bearer'" :label="t('auto.authName')">
                <UInput v-model="form.config.auth_name" class="w-full font-mono" :placeholder="form.config.auth === 'header' ? 'X-Office-Token' : 'token'" />
              </UFormField>
            </div>
            <p class="text-xs text-(--ui-text-muted)">{{ t('auto.tunnelHint') }}</p>
          </template>
        </section>

        <!-- 2. action -->
        <section class="space-y-3">
          <p class="text-sm font-medium">{{ t('auto.stepAction') }}</p>
          <div class="grid grid-cols-2 gap-2">
            <button
              v-for="x in (['task', 'chat'] as const)" :key="x" type="button"
              class="flex items-center gap-2 rounded-lg border px-3 py-2 text-sm font-medium"
              :class="form.action === x ? 'border-primary bg-primary/5' : 'border-(--ui-border)'"
              @click="form.action = x"
            >
              <UIcon :name="x === 'task' ? 'i-lucide-list-todo' : 'i-lucide-messages-square'" class="size-4" />
              {{ x === 'task' ? t('auto.actionTask') : t('auto.actionChat') }}
            </button>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <USelect v-if="form.action === 'chat'" v-model="form.agent_id" :items="agentOptions" class="min-w-48" />
            <EditModePicker v-model="form.edit_mode" />
          </div>
          <USwitch v-if="form.action === 'chat'" v-model="form.keep_context" :label="t('auto.keepContext')" :description="t('auto.keepContextDesc')" />
        </section>

        <!-- 3. content -->
        <section class="space-y-2">
          <p class="text-sm font-medium">{{ t('auto.stepPrompt') }}</p>
          <UTextarea ref="promptEl" v-model="form.prompt" :rows="5" autoresize class="w-full" :placeholder="t('auto.promptPlaceholder')" />
          <div class="flex flex-wrap items-center gap-1 text-xs text-(--ui-text-muted)">
            {{ t('auto.insert') }}
            <button v-for="p in placeholders" :key="p" type="button" class="rounded bg-(--ui-bg-elevated) px-1.5 py-0.5 font-mono hover:text-(--ui-text)" @click="insert(p)">{{ p }}</button>
          </div>
        </section>

        <!-- 4. limits -->
        <details class="space-y-3">
          <summary class="cursor-pointer text-sm font-medium">{{ t('auto.stepLimits') }}</summary>
          <p class="mt-2 text-xs text-(--ui-text-muted)">{{ t('auto.limitsHelp') }}</p>
          <div class="mt-2 grid gap-3 sm:grid-cols-3">
            <UFormField :label="t('auto.maxPerHour')"><UInputNumber v-model="form.limits.max_runs_per_hour" :min="0" class="w-full" /></UFormField>
            <UFormField :label="t('auto.dailyCost')"><UInputNumber v-model="form.limits.daily_cost_usd" :min="0" :step="0.5" class="w-full" /></UFormField>
            <UFormField :label="t('auto.disableAfter')"><UInputNumber v-model="form.limits.disable_after_failures" :min="0" class="w-full" /></UFormField>
          </div>
          <div v-if="form.source === 'webhook'" class="mt-2 grid gap-3 sm:grid-cols-3">
            <UFormField :label="t('auto.debounce')"><UInputNumber v-model="form.limits.debounce_seconds" :min="0" :max="3600" class="w-full" /></UFormField>
            <UFormField :label="t('auto.debounceKey')" :hint="t('auto.debounceKeyHelp')"><UInput v-model="form.limits.debounce_key" class="w-full font-mono" placeholder="issue.key" /></UFormField>
            <UFormField :label="t('auto.debounceMax')"><UInputNumber v-model="form.limits.debounce_max_seconds" :min="0" class="w-full" /></UFormField>
          </div>
        </details>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" :label="t('org.form.close')" @click="open = false" />
        <UButton type="submit" form="automation-form" :loading="saving" :label="t('auto.save')" />
      </div>
    </template>
  </USlideover>

  <WebhookSecretModal :value="secret" @close="secret = null" />
</template>
