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
// the bot whose messages start it (ADR-049): one the project has, or a new one
// made on Save ("" = new); its settings are edited right here
const { data: channelsData } = useFetch<{ channels: Channel[] }>(() => `/api/projects/${props.projectId}/channels`, { lazy: true, immediate: isAdmin.value })
const bots = computed(() => channelsData.value?.channels ?? [])
const fromChannel = computed(() => isChannelSource(form.source))
const NEW_BOT = '__new'
const botName = (c: Channel) => `${c.bot_name ? `@${c.bot_name}` : c.name} · ${c.kind === 'discord' ? 'Discord' : 'Telegram'}`
const botPick = computed({
  get: () => form.config.channel_id || NEW_BOT,
  set: (id: string) => {
    const c = bots.value.find(b => b.id === id)
    form.config.channel_id = c?.id ?? ''
    form.bot = c ? { token: '', allow: [...c.allow], refusal: c.refusal } : { token: '', allow: [], refusal: '' }
    if (c) form.source = c.kind
  }
})
const bot = computed(() => bots.value.find(b => b.id === form.config.channel_id))
const allowText = computed({ get: () => form.bot.allow.join('\n'), set: (v: string) => { form.bot.allow = v.split(/[\n,]/).map(s => s.trim()).filter(Boolean) } })
// the three sources, as cards
const sourceCards = computed(() => [
  { value: 'schedule', icon: 'i-lucide-alarm-clock', title: t('auto.sourceSchedule'), desc: t('auto.sourceScheduleDesc'), active: form.source === 'schedule',
    pick: () => { form.source = 'schedule'; if (form.action === 'chat') form.action = 'task' } },
  { value: 'webhook', icon: 'i-lucide-webhook', title: t('auto.sourceWebhook'), desc: t('auto.sourceWebhookDesc'), active: form.source === 'webhook',
    pick: () => { form.source = 'webhook'; if (form.action === 'chat') form.action = 'task' } },
  { value: 'channel', icon: 'i-lucide-messages-square', title: t('auto.sourceChannel'), desc: t('auto.sourceChannelDesc'), active: fromChannel.value, pick: useChannel }
])
const botMissing = computed(() => (!form.config.channel_id && !form.bot.token) || !form.bot.allow.length)
const guideOpen = ref(false)
const botOpen = ref(false) // a bot it has: its settings folded
const sharedBy = computed(() => bot.value ? (autosData.value?.automations ?? []).filter(a => a.config.channel_id === bot.value!.id).length : 0)
// which messages: by keyword/topic, or a custom slash command
const listenBy = ref<'message' | 'command'>(form.config.command ? 'command' : 'message')
watch(listenBy, (v) => {
  if (v === 'message') Object.assign(form.config, { command: '', command_description: '', command_arg: '' })
  else Object.assign(form.config, { keywords: [], scope: '' })
})
// a name as the "/" menus take it (the server makes it safe the same way)
const commandName = (s: string) => s.replace(/^\/+/, '').normalize('NFD').replace(/[\u0300-\u036f]/g, '').replace(/đ/gi, 'd') // i18n-ignore: the letter đ, not text
  .toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 32)
const takesText = computed({
  get: () => !!form.config.command_arg,
  set: (v: boolean) => { form.config.command_arg = v ? (form.config.command_arg || t('auto.cmdArgDefault')) : '' }
})
// the bot's menu: its own commands (fixed) and the custom ones of its automations
const { data: autosData } = useFetch<{ automations: Automation[] }>(() => `/api/projects/${props.projectId}/automations`, { lazy: true })
const builtinCommands = computed(() => [
  { name: '@', arg: t('cmd.tagArg'), desc: t('cmd.tag') },
  { name: 'job', arg: t('cmd.jobArg'), desc: t('cmd.job') },
  { name: 'create-conversation', arg: '', desc: t('cmd.create') },
  { name: 'close-conversation', arg: '', desc: t('cmd.close') }
])
const customCommands = computed(() => (autosData.value?.automations ?? [])
  .filter(a => isChannelSource(a.source) && a.config.command && a.config.channel_id && a.config.channel_id === form.config.channel_id)
  .map(a => ({ id: a.id, name: a.config.command!, arg: a.config.command_arg ?? '', desc: a.config.command_description || a.name })))
const automationId = computed(() => String(useRoute().params.aid ?? ''))
const cmdLabel = (name: string) => form.source === 'telegram' ? name.replace(/-/g, '_') : name
function useChannel() {
  form.source = bot.value?.kind ?? bots.value[0]?.kind ?? 'telegram'
  if (!form.config.channel_id && bots.value[0]) botPick.value = bots.value[0].id
  if (form.action === 'task' && !form.prompt) form.action = 'chat'
}
const keywords = computed({ get: () => (form.config.keywords ?? []).join(', '), set: (v: string) => { form.config.keywords = v.split(/[,\n]/).map(s => s.trim()).filter(Boolean) } })
// a Select item cannot have "" as its value: "the lead" is a sentinel
const LEAD = '__lead'
// a task goes to the team (the lead splits it) or to one agent, like a person's daily job
const agentOptions = computed(() => [{ label: t('auto.assignTeam'), value: LEAD }, ...(agentsData.value?.agents ?? []).map(a => ({ label: a.name, value: a.id }))])
const chatAgent = computed({ get: () => form.agent_id || LEAD, set: (v: string) => { form.agent_id = v === LEAD ? '' : v } })
// who answers in a chat: one agent ("" = the lead)
const replyAgentOptions = computed(() => [{ label: t('channels.agentLead'), value: LEAD }, ...(agentsData.value?.agents ?? []).map(a => ({ label: a.name, value: a.id }))])
const escalateAgent = computed({ get: () => form.escalate.agent_id || LEAD, set: (v: string) => { form.escalate.agent_id = v === LEAD ? '' : v } })
const langOptions = [{ label: 'bash', value: 'bash' }, { label: 'node', value: 'node' }, { label: 'python', value: 'python' }]
const whenOptions = computed(() => (['failure', 'signal', 'never'] as const).map(v => ({ label: t(`auto.escalate.${v}`), value: v })))

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
  ...(fromChannel.value ? [{ value: 'chat' as const, icon: 'i-lucide-message-circle-reply', title: t('auto.cardReply'), desc: t('auto.cardReplyDesc') }] : []),
  { value: 'script' as const, icon: 'i-lucide-square-terminal', title: t('auto.cardScript'), desc: fromChannel.value ? t('auto.cardScriptChannelDesc') : t('auto.cardScriptDesc') },
  { value: 'task' as const, icon: 'i-lucide-list-todo', title: t('auto.cardTask'), desc: t('auto.cardTaskDesc') }
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

const placeholders = computed(() => fromChannel.value
  ? ['{{message}}', '{{user}}', '{{now}}', '{{today}}', '{{automation}}']
  : ['{{payload}}', '{{payload.x}}', '{{now}}', '{{today}}', '{{yesterday}}', '{{source}}', '{{automation}}'])
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
      <p class="flex items-center gap-2 text-sm font-semibold">
        <span class="flex size-5 items-center justify-center rounded-full bg-primary/15 text-xs text-primary">1</span>{{ t('auto.stepSource') }}
        <span class="font-normal text-(--ui-text-muted)">· {{ t('auto.stepSourceHint') }}</span>
      </p>
      <!-- where it starts: the first choice, so big -->
      <div class="grid gap-2 @md:grid-cols-3">
        <button
          v-for="s in sourceCards" :key="s.value" type="button"
          class="flex items-center gap-2.5 rounded-lg border px-3 py-3 text-left transition"
          :class="s.active ? 'border-primary bg-primary/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
          @click="s.pick()"
        >
          <UIcon :name="s.icon" class="size-5 shrink-0" :class="s.active ? 'text-primary' : 'text-(--ui-text-muted)'" />
          <span class="min-w-0 flex-1 font-medium">{{ s.title }}</span>
          <UTooltip :text="s.desc">
            <UIcon name="i-lucide-info" class="size-4 shrink-0 text-(--ui-text-dimmed) hover:text-(--ui-text)" @click.stop />
          </UTooltip>
        </button>
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
      <template v-else-if="fromChannel">
        <!-- the bot: one the project has, or a new one; how it is doing -->
        <div class="flex flex-wrap items-end gap-3">
          <UFormField v-if="bots.length" :label="t('auto.bot')" required class="min-w-64 flex-1">
            <USelect v-model="botPick" :items="[...bots.map(c => ({ label: botName(c), value: c.id })), { label: t('auto.botNew'), value: NEW_BOT }]" class="w-full" />
          </UFormField>
          <span v-if="bot" class="flex items-center gap-1.5 pb-2 text-xs" :class="bot.last_error ? 'text-(--ui-error)' : 'text-(--ui-text-muted)'">
            <span class="size-1.5 rounded-full" :class="bot.last_error ? 'bg-(--ui-error)' : bot.bot_name ? 'bg-(--ui-success)' : 'bg-(--ui-warning)'" />
            {{ bot.last_error || (bot.bot_name ? t('auto.botRunning') : t('channels.connecting')) }}
          </span>
        </div>

        <!-- the bot's settings: shared by its automations; folded for a bot it has -->
        <div class="flex items-center gap-2">
          <UButton
            v-if="form.config.channel_id" size="xs" color="neutral" variant="ghost" :icon="botOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
            :label="t('auto.tabBot')" @click="botOpen = !botOpen"
          />
          <span v-if="botMissing" class="size-1.5 rounded-full bg-(--ui-error)" />
          <UButton
            class="ms-auto" size="xs" color="neutral" variant="ghost" icon="i-lucide-info"
            :label="form.source === 'discord' ? t('channels.guideDiscord') : t('channels.guideTelegram')" @click="guideOpen = true"
          />
        </div>
        <UModal v-model:open="guideOpen" :title="form.source === 'discord' ? t('channels.guideDiscord') : t('channels.guideTelegram')">
          <template #body><BotGuide :kind="form.source === 'discord' ? 'discord' : 'telegram'" plain /></template>
        </UModal>

        <!-- tab: the bot, shared -->
        <div v-show="!form.config.channel_id || botOpen" class="space-y-3 rounded-lg bg-(--ui-bg-elevated)/30 p-3" :class="hl('bot')">
          <p v-if="form.config.channel_id && (bot?.id && sharedBy > 1)" class="flex items-center gap-1.5 text-xs text-(--ui-warning)">
            <UIcon name="i-lucide-triangle-alert" class="size-3.5" />{{ t('auto.botSharedN', { n: sharedBy }) }}
          </p>
          <template v-if="!form.config.channel_id">
            <div class="flex rounded-lg bg-(--ui-bg-elevated) p-0.5 text-sm">
              <button
                v-for="k in (['telegram', 'discord'] as const)" :key="k" type="button" class="flex flex-1 items-center justify-center gap-1.5 rounded-md py-1"
                :class="form.source === k ? 'bg-(--ui-bg) font-medium shadow-sm' : 'text-(--ui-text-muted)'" @click="form.source = k"
              >
                <UIcon :name="k === 'discord' ? 'i-lucide-gamepad-2' : 'i-lucide-send'" class="size-4" />{{ k === 'discord' ? 'Discord' : 'Telegram' }}
              </button>
            </div>
          </template>
          <UFormField :label="t('channels.token')" :help="form.source === 'discord' ? t('channels.tokenHelpDiscord') : t('channels.tokenHelpTelegram')" :required="!form.config.channel_id">
            <UInput
              v-model="form.bot.token" type="password" name="bot-token" autocomplete="new-password" class="w-full font-mono"
              :placeholder="form.config.channel_id ? t('channels.tokenKept') : (form.source === 'discord' ? 'MTI3…' : '123456789:AAF…')"
            />
          </UFormField>
          <UFormField :label="t('channels.allow')" :help="form.source === 'discord' ? t('channels.allowHelpDiscord') : t('channels.allowHelpTelegram')" required>
            <UTextarea v-model="allowText" :rows="2" autoresize class="w-full font-mono text-xs" :placeholder="form.source === 'discord' ? '123456789012345678' : '123456789'" />
          </UFormField>
          <UFormField :label="t('channels.refusal')" :help="t('channels.refusalHelp')">
            <UInput v-model="form.bot.refusal" class="w-full" :placeholder="t('channels.refusalPlaceholder')" />
          </UFormField>
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
        <span class="flex size-5 items-center justify-center rounded-full bg-primary/15 text-xs text-primary">2</span>{{ fromChannel ? t('auto.stepCommandAction') : t('auto.stepAction') }}
      </p>
      <!-- a bot: the command (or tag) and what it does, together -->
      <div v-if="fromChannel" class="space-y-3" :class="hl('config')">
        <!-- the bot's commands: its own (fixed), the others' custom ones, and this one -->
        <div class="overflow-hidden rounded-lg border border-(--ui-border)">
          <p class="border-b border-(--ui-border) bg-(--ui-bg-elevated)/40 px-3 py-1.5 text-xs font-medium text-(--ui-text-muted)">{{ t('auto.botCommands') }}</p>
          <div v-for="c in builtinCommands" :key="c.name" class="flex items-center gap-2 border-b border-(--ui-border) px-3 py-1.5 text-sm last:border-0">
            <UIcon name="i-lucide-lock" class="size-3.5 shrink-0 text-(--ui-text-dimmed)" />
            <span class="shrink-0 font-mono text-xs">{{ c.name === '@' ? `@${bot?.bot_name || 'bot'}` : `/${cmdLabel(c.name)}` }}<span v-if="c.arg" class="text-(--ui-text-dimmed)"> &lt;{{ c.arg }}&gt;</span></span>
            <span class="min-w-0 flex-1 truncate text-xs text-(--ui-text-muted)">{{ c.desc }}</span>
          </div>
          <NuxtLink
            v-for="c in customCommands.filter(x => x.id !== automationId)" :key="c.id" :to="`/projects/${projectId}/automations/${c.id}`"
            class="flex items-center gap-2 border-b border-(--ui-border) px-3 py-1.5 text-sm last:border-0 hover:bg-(--ui-bg-elevated)/60"
          >
            <UIcon name="i-lucide-square-slash" class="size-3.5 shrink-0 text-(--ui-text-muted)" />
            <span class="shrink-0 font-mono text-xs">/{{ cmdLabel(c.name) }}<span v-if="c.arg" class="text-(--ui-text-dimmed)"> &lt;{{ c.arg }}&gt;</span></span>
            <span class="min-w-0 flex-1 truncate text-xs text-(--ui-text-muted)">{{ c.desc }}</span>
            <UIcon name="i-lucide-arrow-up-right" class="size-3.5 shrink-0 text-(--ui-text-dimmed)" />
          </NuxtLink>
          <div v-if="listenBy === 'command' && form.config.command" class="flex items-center gap-2 bg-primary/5 px-3 py-1.5 text-sm">
            <UIcon name="i-lucide-pencil" class="size-3.5 shrink-0 text-primary" />
            <span class="shrink-0 font-mono text-xs">/{{ cmdLabel(form.config.command) }}<span v-if="form.config.command_arg" class="text-(--ui-text-dimmed)"> &lt;{{ form.config.command_arg }}&gt;</span></span>
            <span class="min-w-0 flex-1 truncate text-xs text-(--ui-text-muted)">{{ form.config.command_description || form.name }}</span>
            <UBadge :label="t('auto.editingThis')" color="primary" variant="subtle" size="sm" />
          </div>
        </div>

          <div class="flex rounded-lg bg-(--ui-bg-elevated) p-0.5 text-sm">
            <button
              v-for="k in (['message', 'command'] as const)" :key="k" type="button" class="flex flex-1 items-center justify-center gap-1.5 rounded-md py-1"
              :class="listenBy === k ? 'bg-(--ui-bg) font-medium shadow-sm' : 'text-(--ui-text-muted)'" @click="listenBy = k"
            >
              <UIcon :name="k === 'command' ? 'i-lucide-square-slash' : 'i-lucide-at-sign'" class="size-4" />{{ k === 'command' ? t('auto.byCommand') : t('auto.byMessage') }}
            </button>
          </div>
          <template v-if="listenBy === 'message'">
            <div class="grid gap-3 @lg:grid-cols-2">
              <UFormField :label="t('auto.keywords')" :help="t('auto.keywordsHelp')">
                <UInput v-model="keywords" class="w-full" :placeholder="t('auto.keywordsPlaceholder')" />
              </UFormField>
              <UFormField :label="t('auto.scope')" :help="t('auto.scopeHelp')">
                <UInput v-model="form.config.scope" class="w-full" :placeholder="t('auto.scopePlaceholder')" />
              </UFormField>
            </div>
            <p class="text-xs text-(--ui-text-muted)">{{ t('auto.ruleOrder') }}</p>
          </template>
          <template v-else>
            <div class="grid gap-3 @lg:grid-cols-2">
              <UFormField :label="t('auto.cmdName')" :help="t('auto.cmdNameHelp')" required>
                <UInput
                  :model-value="form.config.command" class="w-full font-mono" placeholder="don-hang"
                  @update:model-value="(v: string | number) => { form.config.command = commandName(String(v)) }"
                >
                  <template #leading><span class="font-mono text-(--ui-text-muted)">/</span></template>
                </UInput>
              </UFormField>
              <UFormField :label="t('auto.cmdDescription')">
                <UInput v-model="form.config.command_description" class="w-full" :placeholder="t('auto.cmdDescriptionPlaceholder')" />
              </UFormField>
            </div>
            <div class="flex flex-wrap items-center gap-3">
              <UCheckbox v-model="takesText" :label="t('auto.cmdTakesText')" />
              <UInput v-if="takesText" v-model="form.config.command_arg" size="sm" class="w-48" :placeholder="t('auto.cmdArgDefault')" />
            </div>
            <!-- how it looks in the "/" menu -->
            <div v-if="form.config.command" class="rounded-lg border border-(--ui-border) bg-(--ui-bg-elevated)/40 p-3">
              <p class="mb-1 text-xs text-(--ui-text-muted)">{{ t('auto.cmdPreview', { app: form.source === 'discord' ? 'Discord' : 'Telegram' }) }}</p>
              <p class="font-mono text-sm">
                /{{ cmdLabel(form.config.command) }}
                <span v-if="form.config.command_arg" class="ms-1 rounded bg-(--ui-bg-accented) px-1.5 py-0.5 text-xs">{{ form.config.command_arg }}</span>
              </p>
              <p class="text-xs text-(--ui-text-muted)">{{ form.config.command_description || form.name }} · {{ bot?.bot_name ? `@${bot.bot_name}` : t('auto.bot') }}</p>
              <p v-if="form.config.command_arg" class="mt-2 text-xs text-(--ui-text-muted)">{{ t('auto.cmdArgHint') }}</p>
            </div>
          </template>
        </div>
        <p v-if="fromChannel" class="flex items-center gap-1.5 pt-1 text-sm font-medium"><UIcon name="i-lucide-corner-down-right" class="size-4 text-(--ui-text-muted)" />{{ t('auto.thenDo') }}</p>
      <div class="grid gap-2 @md:grid-cols-2">
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
            <UFormField v-if="form.escalate.when !== 'never'" :label="t('auto.assignTo')"><USelect v-model="escalateAgent" :items="agentOptions" class="w-full" /></UFormField>
          </div>
          <UFormField v-if="form.escalate.when !== 'never'" :label="t('auto.escalatePrompt')">
            <UTextarea v-model="form.escalate.prompt" :rows="3" autoresize class="w-full" :placeholder="t('auto.escalatePromptPlaceholder')" />
          </UFormField>
        </div>
      </template>
      <div v-else-if="form.action === 'chat'" class="space-y-2">
        <div class="flex flex-wrap items-end gap-3">
          <UFormField :label="t('channels.agent')"><USelect v-model="chatAgent" :items="replyAgentOptions" class="min-w-56" /></UFormField>
        </div>
        <p class="text-xs text-(--ui-text-muted)">{{ t('auto.replyNoTools') }}</p>
      </div>
      <div v-else class="flex flex-wrap items-end gap-3">
        <UFormField :label="t('auto.assignTo')"><USelect v-model="chatAgent" :items="agentOptions" class="min-w-56" /></UFormField>
        <EditModePicker v-model="form.edit_mode" />
      </div>
    </section>

    <!-- 3. content -->
    <section v-if="form.action !== 'script'" class="space-y-3 rounded-xl border border-(--ui-border) p-4" :class="hl('prompt')">
      <p class="flex items-center gap-2 text-sm font-semibold">
        <span class="flex size-5 items-center justify-center rounded-full bg-primary/15 text-xs text-primary">3</span>{{ t('auto.stepPrompt') }}
      </p>
      <UTextarea ref="promptEl" v-model="form.prompt" :rows="5" autoresize class="w-full" :placeholder="fromChannel ? t('auto.promptChannelPlaceholder') : t('auto.promptPlaceholder')" />
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
