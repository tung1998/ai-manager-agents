<script setup lang="ts">
// The automation form: trigger, action, content, limits (ADR-042). It edits
// the draft it is given; the builder page saves it. highlight marks fields an
// agent just changed; "Chạy thử" runs the script now without saving.
const props = defineProps<{ projectId: string, form: AutomationDraft, highlight?: string[] }>()
const emit = defineEmits<{ tested: [{ output: string, exit_code: number, timed_out: boolean }] }>()
const toast = useToast()
const { t, dateLocale } = useLang()
// eslint-disable-next-line vue/no-mutating-props -- the draft is the parent's reactive object, edited in place
const form = props.form

const { data: agentsData } = useFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
const agentOptions = computed(() => [{ label: t('auto.agentDefault'), value: '' }, ...(agentsData.value?.agents ?? []).map(a => ({ label: a.name, value: a.id }))])
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
  <div class="space-y-5">
        <UFormField :label="t('auto.name')" required :class="hl('name')">
          <UInput v-model="form.name" class="w-full" :placeholder="t('auto.namePlaceholder')" />
        </UFormField>

        <!-- 1. trigger -->
        <section class="space-y-3" :class="hl('source') || hl('config')">
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
        <section class="space-y-3" :class="hl('action') || hl('escalate')">
          <p class="text-sm font-medium">{{ t('auto.stepAction') }}</p>
          <div class="grid grid-cols-3 gap-2">
            <button
              v-for="x in (['script', 'task', 'chat'] as const)" :key="x" type="button"
              class="flex items-center gap-2 rounded-lg border px-3 py-2 text-left text-sm font-medium"
              :class="form.action === x ? 'border-primary bg-primary/5' : 'border-(--ui-border)'"
              @click="form.action = x"
            >
              <UIcon :name="x === 'task' ? 'i-lucide-list-todo' : x === 'chat' ? 'i-lucide-messages-square' : 'i-lucide-square-terminal'" class="size-4 shrink-0" />
              {{ x === 'task' ? t('auto.actionTask') : x === 'chat' ? t('auto.actionChat') : t('auto.actionScript') }}
            </button>
          </div>
          <template v-if="form.action === 'script'">
            <div class="flex flex-wrap gap-3">
              <UFormField :label="t('auto.scriptLang')"><USelect v-model="form.script.lang" :items="langOptions" class="w-28" /></UFormField>
              <UFormField :label="t('auto.timeout')"><UInputNumber v-model="form.script.timeout_s" :min="1" :max="3600" class="w-32" /></UFormField>
            </div>
            <UFormField :label="t('auto.scriptBody')" :help="t('auto.scriptHelp')" :class="hl('script')">
              <UTextarea v-model="form.script.body" :rows="12" autoresize class="w-full font-mono text-xs" placeholder="grep -c ERROR logs/app.log || true" />
            </UFormField>
            <div class="space-y-2 rounded-lg border border-(--ui-border) p-3">
              <div class="flex flex-wrap items-center gap-2">
                <UInput v-model="testPayload" size="xs" class="min-w-0 flex-1 font-mono" :placeholder="t('auto.testPayload')" />
                <UButton size="xs" icon="i-lucide-play" :label="t('auto.testRun')" :loading="testing" :disabled="!form.script.body.trim()" @click="testRun" />
              </div>
              <template v-if="tested">
                <p class="text-xs" :class="tested.exit_code === 0 ? 'text-(--ui-success)' : 'text-(--ui-error)'">
                  {{ tested.timed_out ? t('auto.testTimeout') : t('job.exit', { n: tested.exit_code }) }}
                </p>
                <pre class="max-h-60 overflow-auto rounded bg-(--ui-bg-elevated) p-2 font-mono text-xs">{{ tested.output || t('job.noOutput') }}</pre>
              </template>
            </div>
            <UFormField :label="t('auto.escalateWhen')"><USelect v-model="form.escalate.when" :items="whenOptions" class="w-full" /></UFormField>
            <template v-if="form.escalate.when !== 'never'">
              <div class="grid gap-3 sm:grid-cols-2">
                <UFormField :label="t('auto.escalateAction')"><USelect v-model="form.escalate.action" :items="escalateActionOptions" class="w-full" /></UFormField>
                <UFormField v-if="form.escalate.action === 'chat'" :label="t('auto.escalateAgent')"><USelect v-model="form.escalate.agent_id" :items="agentOptions" class="w-full" /></UFormField>
              </div>
              <UFormField :label="t('auto.escalatePrompt')">
                <UTextarea v-model="form.escalate.prompt" :rows="3" autoresize class="w-full" :placeholder="t('auto.escalatePromptPlaceholder')" />
              </UFormField>
            </template>
          </template>
          <div v-if="form.action !== 'script'" class="flex flex-wrap items-center gap-3">
            <USelect v-if="form.action === 'chat'" v-model="form.agent_id" :items="agentOptions" class="min-w-48" />
            <EditModePicker v-model="form.edit_mode" />
          </div>
          <USwitch v-if="form.action === 'chat'" v-model="form.keep_context" :label="t('auto.keepContext')" :description="t('auto.keepContextDesc')" />
        </section>

        <!-- 3. content -->
        <section v-if="form.action !== 'script'" class="space-y-2" :class="hl('prompt')">
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
      </div>
</template>
