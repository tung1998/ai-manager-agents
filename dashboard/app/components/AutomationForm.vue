<script setup lang="ts">
// The automation form: trigger, action, content, limits (ADR-042). It edits
// the draft it is given; the builder page saves it. highlight marks fields an
// agent just changed; "Chạy thử" runs the script now without saving.
const props = defineProps<{ projectId: string, form: AutomationDraft, highlight?: string[] }>()
const emit = defineEmits<{ tested: [{ output: string, exit_code: number, timed_out: boolean }] }>()
const toast = useToast()
const { isAdmin } = useAuth()
const { t, dateLocale } = useLang()
// eslint-disable-next-line vue/no-mutating-props -- the draft is the parent's reactive object, edited in place
const form = props.form

const { data: agentsData } = useFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
// a Select item cannot have "" as its value: "the lead" is a sentinel
const LEAD = '__lead'
const agentOptions = computed(() => [{ label: t('auto.agentDefault'), value: LEAD }, ...(agentsData.value?.agents ?? []).map(a => ({ label: a.name, value: a.id }))])
const chatAgent = computed({ get: () => form.agent_id || LEAD, set: (v: string) => { form.agent_id = v === LEAD ? '' : v } })
const escalateAgent = computed({ get: () => form.escalate.agent_id || LEAD, set: (v: string) => { form.escalate.agent_id = v === LEAD ? '' : v } })
const langOptions = [{ label: 'bash', value: 'bash' }, { label: 'node', value: 'node' }, { label: 'python', value: 'python' }]
const whenOptions = computed(() => (['failure', 'signal', 'never'] as const).map(v => ({ label: t(`auto.escalate.${v}`), value: v })))
const escalateActionOptions = computed(() => [{ label: t('auto.actionChat'), value: 'chat' }, { label: t('auto.actionTask'), value: 'task' }])

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
// a schedule either repeats every N minutes or runs at fixed times (cron)
const schedKind = computed({
  get: () => (form.config.every_minutes ?? 0) > 0 ? 'every' : 'cron',
  set: (v: string) => {
    if (v === 'every') {
      form.config.cron = ''
      form.config.every_minutes = form.config.every_minutes || 15
    } else {
      form.config.every_minutes = 0
      form.config.cron = form.config.cron || '0 8 * * *'
    }
  }
})
const presetActive = (p: { every: number, cron: string }) => (form.config.every_minutes ?? 0) === p.every && (form.config.cron ?? '') === p.cron
// every IANA zone the browser/Node knows, searchable
const timezones = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') ?? ['UTC', 'Asia/Ho_Chi_Minh']
const tz = computed({ get: () => form.config.timezone || 'Asia/Ho_Chi_Minh', set: (v: string) => { form.config.timezone = v } })
// the saved zone stays pickable even if this runtime spells it differently
const tzItems = computed(() => timezones.includes(tz.value) ? timezones : [tz.value, ...timezones])
const actions = computed(() => [
  { value: 'script' as const, icon: 'i-lucide-square-terminal', title: t('auto.cardScript'), desc: t('auto.cardScriptDesc') },
  { value: 'task' as const, icon: 'i-lucide-list-todo', title: t('auto.cardTask'), desc: t('auto.cardTaskDesc') },
  { value: 'chat' as const, icon: 'i-lucide-messages-square', title: t('auto.cardChat'), desc: t('auto.cardChatDesc') }
])

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

const hl = (key: string) => props.highlight?.includes(key) ? 'rounded-lg ring-2 ring-primary/60 ring-offset-2 ring-offset-(--ui-bg) transition' : ''

// "Chạy thử": the script runs now, not saved
const testPayload = ref('')
const testing = ref(false)
const tested = ref<{ output: string, exit_code: number, timed_out: boolean } | null>(null)
async function testRun() {
  testing.value = true
  try {
    tested.value = await $fetch(`/api/projects/${props.projectId}/automations/test-script`, { method: 'POST', body: { script: form.script, payload: testPayload.value } })
    emit('tested', tested.value!)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    testing.value = false
  }
}
</script>

<template>
  <!-- sized by its own width: the form sits in half of the builder page -->
  <div class="@container space-y-4">
    <UFormField :label="t('auto.name')" required :class="hl('name')">
      <UInput v-model="form.name" class="w-full" :placeholder="t('auto.namePlaceholder')" />
    </UFormField>

    <!-- 1. trigger -->
    <section class="space-y-4 rounded-xl border border-(--ui-border) p-4" :class="hl('source') || hl('config')">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="flex items-center gap-2 text-sm font-semibold">
          <span class="flex size-5 items-center justify-center rounded-full bg-primary/15 text-xs text-primary">1</span>{{ t('auto.stepSource') }}
        </p>
        <div class="flex rounded-lg bg-(--ui-bg-elevated) p-0.5">
          <button
            v-for="s in (['schedule', 'webhook'] as const)" :key="s" type="button"
            class="flex items-center gap-1.5 rounded-md px-3 py-1 text-sm"
            :class="form.source === s ? 'bg-(--ui-bg) font-medium shadow-sm' : 'text-(--ui-text-muted) hover:text-(--ui-text)'"
            @click="form.source = s"
          >
            <UIcon :name="s === 'schedule' ? 'i-lucide-alarm-clock' : 'i-lucide-webhook'" class="size-4" />
            {{ s === 'schedule' ? t('auto.sourceSchedule') : t('auto.sourceWebhook') }}
          </button>
        </div>
      </div>

      <template v-if="form.source === 'schedule'">
        <div class="flex flex-wrap gap-1.5">
          <UButton
            v-for="p in presets" :key="p.label" size="xs" :color="presetActive(p) ? 'primary' : 'neutral'" :variant="presetActive(p) ? 'soft' : 'outline'"
            :label="p.label" @click="usePreset(p)"
          />
        </div>
        <div class="grid gap-3 @lg:grid-cols-2">
          <UFormField :label="schedKind === 'every' ? t('auto.schedEvery') : t('auto.schedCron')" :help="schedKind === 'cron' ? t('auto.cronHelp') : undefined">
            <div class="flex items-center gap-2">
              <USelect v-model="schedKind" :items="[{ label: t('auto.schedEvery'), value: 'every' }, { label: t('auto.schedCron'), value: 'cron' }]" class="w-36 shrink-0" />
              <template v-if="schedKind === 'every'">
                <UInputNumber v-model="form.config.every_minutes" :min="1" class="w-32" />
                <span class="text-sm text-(--ui-text-muted)">{{ t('auto.unitMinutes') }}</span>
              </template>
              <UInput v-else v-model="form.config.cron" class="min-w-0 flex-1 font-mono" placeholder="0 8 * * 1-5" />
            </div>
          </UFormField>
          <UFormField :label="t('auto.timezone')">
            <USelectMenu v-model="tz" :items="tzItems" :search-input="{ placeholder: t('auto.tzSearch') }" class="w-full" />
          </UFormField>
        </div>
        <div class="flex flex-wrap items-center gap-1.5 text-xs">
          <span class="me-1 text-(--ui-text-muted)">{{ t('auto.nextRuns') }}</span>
          <span v-if="preview.error" class="text-(--ui-error)">{{ preview.error }}</span>
          <span v-for="n in preview.next" v-else :key="n" class="rounded-md bg-(--ui-bg-elevated) px-2 py-0.5 tabular-nums">{{ fmt(n) }}</span>
        </div>
      </template>
      <template v-else>
        <div class="grid gap-3 @lg:grid-cols-2">
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
    <section class="space-y-4 rounded-xl border border-(--ui-border) p-4" :class="hl('action') || hl('escalate')">
      <p class="flex items-center gap-2 text-sm font-semibold">
        <span class="flex size-5 items-center justify-center rounded-full bg-primary/15 text-xs text-primary">2</span>{{ t('auto.stepAction') }}
      </p>
      <div class="grid gap-2 @md:grid-cols-3">
        <button
          v-for="x in actions" :key="x.value" type="button"
          class="flex h-full items-start gap-2.5 rounded-lg border p-3 text-left transition"
          :class="form.action === x.value ? 'border-primary bg-primary/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
          @click="form.action = x.value"
        >
          <UIcon :name="x.icon" class="mt-0.5 size-4 shrink-0" :class="form.action === x.value ? 'text-primary' : 'text-(--ui-text-muted)'" />
          <span class="min-w-0">
            <span class="block text-sm font-medium">{{ x.title }}</span>
            <span class="block text-xs text-(--ui-text-muted)">{{ x.desc }}</span>
          </span>
        </button>
      </div>

      <template v-if="form.action === 'script'">
        <div class="flex flex-wrap items-end gap-3">
          <UFormField :label="t('auto.scriptLang')"><USelect v-model="form.script.lang" :items="langOptions" class="w-32" /></UFormField>
          <UFormField :label="t('auto.timeout')">
            <div class="flex items-center gap-2">
              <UInputNumber v-model="form.script.timeout_s" :min="1" :max="3600" class="w-32" />
              <span class="text-sm text-(--ui-text-muted)">{{ t('auto.unitSeconds') }}</span>
            </div>
          </UFormField>
        </div>
        <UFormField :label="t('auto.scriptBody')" :help="t('auto.scriptHelp')" :class="hl('script')">
          <UTextarea v-model="form.script.body" :rows="12" autoresize class="w-full font-mono text-xs" placeholder="grep -c ERROR logs/app.log || true" />
        </UFormField>
        <div class="space-y-2 rounded-lg bg-(--ui-bg-elevated)/50 p-3">
          <div class="flex flex-wrap items-center gap-2">
            <UInput v-model="testPayload" size="sm" class="min-w-0 flex-1 font-mono" :placeholder="t('auto.testPayload')" />
            <UButton v-if="isAdmin" size="sm" icon="i-lucide-play" :label="t('auto.testRun')" :loading="testing" :disabled="!form.script.body.trim()" @click="testRun" />
          </div>
          <template v-if="tested">
            <p class="text-xs" :class="tested.exit_code === 0 ? 'text-(--ui-success)' : 'text-(--ui-error)'">
              {{ tested.timed_out ? t('auto.testTimeout') : t('job.exit', { n: tested.exit_code }) }}
            </p>
            <pre class="max-h-60 overflow-auto rounded bg-(--ui-bg) p-2 font-mono text-xs">{{ tested.output || t('job.noOutput') }}</pre>
          </template>
        </div>
        <div class="space-y-3 rounded-lg border border-dashed border-(--ui-border) p-3">
          <p class="flex items-center gap-1.5 text-sm font-medium">
            <UIcon name="i-lucide-sparkles" class="size-4 text-primary" />{{ t('auto.escalateTitle') }}
          </p>
          <div class="grid gap-3 @lg:grid-cols-3">
            <UFormField :label="t('auto.escalateWhen')"><USelect v-model="form.escalate.when" :items="whenOptions" class="w-full" /></UFormField>
            <template v-if="form.escalate.when !== 'never'">
              <UFormField :label="t('auto.escalateAction')"><USelect v-model="form.escalate.action" :items="escalateActionOptions" class="w-full" /></UFormField>
              <UFormField v-if="form.escalate.action === 'chat'" :label="t('auto.escalateAgent')"><USelect v-model="escalateAgent" :items="agentOptions" class="w-full" /></UFormField>
            </template>
          </div>
          <UFormField v-if="form.escalate.when !== 'never'" :label="t('auto.escalatePrompt')">
            <UTextarea v-model="form.escalate.prompt" :rows="3" autoresize class="w-full" :placeholder="t('auto.escalatePromptPlaceholder')" />
          </UFormField>
        </div>
      </template>
      <template v-else>
        <div class="flex flex-wrap items-center gap-3">
          <USelect v-if="form.action === 'chat'" v-model="chatAgent" :items="agentOptions" class="min-w-48" />
          <EditModePicker v-model="form.edit_mode" />
        </div>
        <USwitch v-if="form.action === 'chat'" v-model="form.keep_context" :label="t('auto.keepContext')" :description="t('auto.keepContextDesc')" />
      </template>
    </section>

    <!-- 3. content -->
    <section v-if="form.action !== 'script'" class="space-y-3 rounded-xl border border-(--ui-border) p-4" :class="hl('prompt')">
      <p class="flex items-center gap-2 text-sm font-semibold">
        <span class="flex size-5 items-center justify-center rounded-full bg-primary/15 text-xs text-primary">3</span>{{ t('auto.stepPrompt') }}
      </p>
      <UTextarea ref="promptEl" v-model="form.prompt" :rows="5" autoresize class="w-full" :placeholder="t('auto.promptPlaceholder')" />
      <div class="flex flex-wrap items-center gap-1 text-xs text-(--ui-text-muted)">
        {{ t('auto.insert') }}
        <button v-for="p in placeholders" :key="p" type="button" class="rounded bg-(--ui-bg-elevated) px-1.5 py-0.5 font-mono hover:text-(--ui-text)" @click="insert(p)">{{ p }}</button>
      </div>
    </section>

    <!-- limits -->
    <details class="group rounded-xl border border-(--ui-border) p-4">
      <summary class="flex cursor-pointer list-none items-center gap-2 text-sm font-semibold">
        <UIcon name="i-lucide-chevron-right" class="size-4 transition group-open:rotate-90" />{{ t('auto.stepLimits') }}
      </summary>
      <p class="mt-2 text-xs text-(--ui-text-muted)">{{ t('auto.limitsHelp') }}</p>
      <div class="mt-3 grid gap-3 @lg:grid-cols-3">
        <UFormField :label="t('auto.maxPerHour')"><UInputNumber v-model="form.limits.max_runs_per_hour" :min="0" class="w-full" /></UFormField>
        <UFormField :label="t('auto.dailyCost')"><UInputNumber v-model="form.limits.daily_cost_usd" :min="0" :step="0.5" class="w-full" /></UFormField>
        <UFormField :label="t('auto.disableAfter')"><UInputNumber v-model="form.limits.disable_after_failures" :min="0" class="w-full" /></UFormField>
      </div>
      <div v-if="form.source === 'webhook'" class="mt-3 grid gap-3 @lg:grid-cols-3">
        <UFormField :label="t('auto.debounce')"><UInputNumber v-model="form.limits.debounce_seconds" :min="0" :max="3600" class="w-full" /></UFormField>
        <UFormField :label="t('auto.debounceKey')" :help="t('auto.debounceKeyHelp')"><UInput v-model="form.limits.debounce_key" class="w-full font-mono" placeholder="issue.key" /></UFormField>
        <UFormField :label="t('auto.debounceMax')"><UInputNumber v-model="form.limits.debounce_max_seconds" :min="0" class="w-full" /></UFormField>
      </div>
    </details>
  </div>
</template>
