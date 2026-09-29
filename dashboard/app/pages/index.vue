<script setup lang="ts">
const { user, isAdmin } = useAuth()
const { t, dateLocale } = useLang()

const _f2 = useFetch<{ providers: Provider[] }>('/api/providers')
const { data: prov } = _f2
const _f3 = useFetch<{ projects: Project[] }>('/api/projects')
const { data: proj } = _f3
const _f4 = useFetch<{ templates: OrgModel[] }>('/api/templates')
const { data: tpl } = _f4

// the latest chats across projects (bots' too): where the work happens now
interface RecentChat { id: string, project_id: string, title: string, agent_name: string, source?: Source, updated_at: string, active_turn?: string }
const _f5 = useFetch<{ conversations: RecentChat[], projects: Record<string, string> }>('/api/conversations/recent', { query: { limit: 6 } })
const { data: chatData } = _f5
const recentChats = computed(() => chatData.value?.conversations ?? [])
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })

// what needs a person, and the last day in numbers
interface Incident { kind: string, severity: 'error' | 'warning', project_id: string, project_name: string, title: string, detail: string, link: string, id: string, key: string }
const _f6 = useFetch<{ incidents: Incident[], count: number }>('/api/incidents')
const { data: incData, refresh: refreshInc } = _f6
let incTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => { incTimer = setInterval(() => refreshInc(), 30000) })
onBeforeUnmount(() => clearInterval(incTimer))
const incidents = computed(() => incData.value?.incidents ?? [])
// the kind of each, as a word next to it
const incKind = (k: string) => t(`incidents.kind.${k}` as 'incidents.kind.monitor')
const _f7 = useFetch<{ totals: { running: number, pending: number, failed_24h: number, cost_24h: number } }>('/api/jobs/stats?since=24h')
const { data: stats } = _f7
await Promise.all([_f2, _f3, _f4, _f5, _f6, _f7]) // started together: one round trip, not 7 (a phone over a VPN)
// what can be done from the list: decide a card; try again, let go or
// investigate something that went wrong
const toast = useToast()
const acting = ref('')
async function act(x: Incident, what: 'approve' | 'reject' | 'retry' | 'dismiss') {
  acting.value = x.key + what
  try {
    if ((what === 'approve' || what === 'reject') && x.kind === 'patch') await $fetch(`/api/patches/${x.id}/${what}`, { method: 'POST' })
    else if (what === 'approve' || what === 'reject') await $fetch(`/api/actions/${x.id}/${what}`, { method: 'POST', body: {} })
    else if (what === 'dismiss') await $fetch('/api/incidents/dismiss', { method: 'POST', body: { key: x.key } })
    else if (x.kind === 'jobs') await $fetch(`/api/jobs/${x.id}/retry`, { method: 'POST' })
    else await $fetch('/api/incidents/retry', { method: 'POST', body: { kind: x.kind, id: x.id } })
    toast.add({ title: t(`home.done_${what}`), color: 'success' })
    await refreshInc()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    acting.value = ''
  }
}
// Điều tra: the project's lead looks into it in a new chat
const prefill = useState<{ text: string, files: [], send?: boolean } | null>('chat-prefill', () => null)
function investigate(x: Incident) {
  prefill.value = { text: t('home.investigatePrompt', { kind: incKind(x.kind), title: x.title, detail: x.detail || '—' }), files: [], send: true }
  navigateTo({ path: `/projects/${x.project_id}`, query: { tab: 'chat' } })
}
const canRetry = (k: string) => ['monitor', 'process', 'automation', 'bot', 'jobs'].includes(k)
const incIcon: Record<string, string> = {
  monitor: 'i-lucide-activity', process: 'i-lucide-square-terminal', automation: 'i-lucide-alarm-clock-off',
  bot: 'i-lucide-bot', jobs: 'i-lucide-circle-x', approval: 'i-lucide-stamp', patch: 'i-lucide-file-diff'
}

const providers = computed(() => prov.value?.providers ?? [])
const projects = computed(() => proj.value?.projects ?? [])
const okProviders = computed(() => providers.value.filter(p => p.status === 'ok').length)
const withModel = computed(() => projects.value.filter(p => p.model).length)

const setupDone = computed(() => steps.value.every(s => s.done))
const steps = computed(() => [
  {
    done: okProviders.value > 0,
    title: t('home.step1Title'),
    text: okProviders.value ? t('home.step1TextDone', { n: okProviders.value, total: providers.value.length }) : t('home.step1TextTodo'),
    to: '/providers', icon: 'i-lucide-plug'
  },
  {
    done: projects.value.length > 0,
    title: t('home.step2Title'),
    text: projects.value.length ? t('home.step2TextDone', { n: projects.value.length }) : t('home.step2TextTodo'),
    to: '/projects', icon: 'i-lucide-folder-git-2'
  },
  {
    done: projects.value.length > 0 && withModel.value === projects.value.length,
    title: t('home.step3Title'),
    text: projects.value.length ? t('home.step3TextDone', { n: withModel.value, total: projects.value.length }) : t('home.step3TextTodo'),
    to: projects.value.length ? '/projects' : '/templates', icon: 'i-lucide-network'
  }
])
</script>

<template>
  <PageShell :title="t('nav.overview')">
    <div class="space-y-6">

      <!-- what needs a person now -->
      <UCard :ui="{ body: 'p-0 sm:p-0' }">
        <template #header>
          <div class="flex items-center justify-between gap-2">
            <p class="flex items-center gap-2 font-semibold">
              <UIcon name="i-lucide-siren" class="size-5" :class="incData?.count ? 'text-(--ui-error)' : 'text-(--ui-success)'" />
              {{ t('home.attention') }}
              <UBadge v-if="incData?.count" :label="String(incData.count)" color="error" variant="subtle" size="sm" />
            </p>

          </div>
        </template>
        <p v-if="!incidents.length" class="flex items-center gap-2 p-4 text-sm text-(--ui-text-muted)">
          <UIcon name="i-lucide-circle-check" class="size-4 text-(--ui-success)" />{{ t('home.allGood') }}
        </p>
        <div v-else class="divide-y divide-(--ui-border)">
          <div v-for="(x, i) in incidents" :key="i" class="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2.5">
            <NuxtLink :to="x.link" class="flex min-w-0 flex-1 basis-60 items-center gap-3">
              <UIcon :name="incIcon[x.kind] ?? 'i-lucide-circle-alert'" class="size-4 shrink-0" :class="x.severity === 'error' ? 'text-(--ui-error)' : 'text-(--ui-warning)'" />
              <span class="min-w-0 flex-1">
                <span class="flex items-center gap-2">
                  <span class="truncate text-sm font-medium hover:underline">{{ x.title }}</span>
                  <UBadge :label="incKind(x.kind)" color="neutral" variant="subtle" size="sm" />
                </span>
                <span class="block truncate text-xs text-(--ui-text-muted)">{{ x.project_name }}<template v-if="x.detail"> · {{ x.detail }}</template></span>
              </span>
            </NuxtLink>
            <!-- what to do about it, right here -->
            <div v-if="isAdmin" class="flex shrink-0 flex-wrap items-center gap-1 max-sm:w-full max-sm:ps-7">
              <template v-if="x.kind === 'approval' || x.kind === 'patch'">
                <UButton size="xs" icon="i-lucide-check" :label="t('home.approve')" :loading="acting === x.key + 'approve'" @click="act(x, 'approve')" />
                <UButton size="xs" color="neutral" variant="ghost" :label="t('home.reject')" :loading="acting === x.key + 'reject'" @click="act(x, 'reject')" />
              </template>
              <template v-else>
                <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-eye" :label="t('home.view')" :to="x.link" />
                <UButton v-if="canRetry(x.kind)" size="xs" color="neutral" variant="outline" icon="i-lucide-rotate-cw" :label="t('home.retry')" :loading="acting === x.key + 'retry'" @click="act(x, 'retry')" />
                <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-search-check" :label="t('home.investigate')" @click="investigate(x)" />
                <UButton size="xs" color="neutral" variant="ghost" :label="t('home.dismiss')" :loading="acting === x.key + 'dismiss'" @click="act(x, 'dismiss')" />
              </template>
            </div>
          </div>
        </div>
      </UCard>

      <!-- the last day -->
      <div class="grid grid-cols-2 gap-2 sm:grid-cols-4 sm:gap-3">
        <UCard :ui="{ body: 'p-3 sm:p-4' }">
          <p class="text-xs text-(--ui-text-muted)">{{ t('job.running') }}</p>
          <p class="text-xl font-semibold tabular-nums sm:text-2xl">{{ (stats?.totals.running ?? 0) + (stats?.totals.pending ?? 0) }}</p>
        </UCard>
        <UCard :ui="{ body: 'p-3 sm:p-4' }">
          <p class="text-xs text-(--ui-text-muted)">{{ t('job.failed24') }}</p>
          <p class="text-xl font-semibold tabular-nums sm:text-2xl" :class="stats?.totals.failed_24h ? 'text-(--ui-error)' : ''">{{ stats?.totals.failed_24h ?? 0 }}</p>
        </UCard>
        <UCard :ui="{ body: 'p-3 sm:p-4' }">
          <p class="text-xs text-(--ui-text-muted)">{{ t('job.cost24') }}</p>
          <p class="text-xl font-semibold tabular-nums sm:text-2xl">${{ (stats?.totals.cost_24h ?? 0).toFixed(2) }}</p>
        </UCard>
        <NuxtLink to="/projects">
          <UCard :ui="{ body: 'p-3 sm:p-4' }" class="h-full hover:bg-(--ui-bg-elevated)/40">
            <p class="text-xs text-(--ui-text-muted)">{{ t('home.project') }}</p>
            <p class="text-xl font-semibold tabular-nums sm:text-2xl">{{ projects.length }}</p>
          </UCard>
        </NuxtLink>
      </div>

      <UCard v-if="!setupDone">
        <template #header>
          <p class="font-medium">{{ t('home.setupTitle') }}</p>
          <p class="text-sm text-(--ui-text-muted)">{{ t('home.setupDesc') }}</p>
        </template>
        <ol class="space-y-3">
          <li v-for="(s, i) in steps" :key="s.title">
            <NuxtLink :to="s.to" class="flex items-center gap-3 rounded-md p-2 transition hover:bg-(--ui-bg-muted)">
              <span
                class="flex size-7 shrink-0 items-center justify-center rounded-full text-sm font-medium"
                :class="s.done ? 'bg-(--ui-success) text-white' : 'bg-(--ui-bg-accented)'"
              >
                <UIcon v-if="s.done" name="i-lucide-check" class="size-4" />
                <span v-else>{{ i + 1 }}</span>
              </span>
              <UIcon :name="s.icon" class="size-5 text-primary" />
              <div class="min-w-0 flex-1">
                <p class="font-medium">{{ s.title }}</p>
                <p class="text-sm text-(--ui-text-muted)">{{ s.text }}</p>
              </div>
              <UIcon name="i-lucide-chevron-right" class="size-4 text-(--ui-text-dimmed)" />
            </NuxtLink>
          </li>
        </ol>
      </UCard>

      <UCard v-if="recentChats.length" :ui="{ body: 'p-0 sm:p-0' }">
        <template #header>
          <p class="font-semibold">{{ t('home.recentChats') }}</p>
        </template>
        <div class="divide-y divide-(--ui-border)">
          <NuxtLink
            v-for="c in recentChats" :key="c.id" :to="`/projects/${c.project_id}?tab=chat&c=${c.id}`"
            class="flex items-center gap-3 px-4 py-2.5 transition hover:bg-(--ui-bg-elevated)"
          >
            <UIcon
              :name="c.active_turn ? 'i-lucide-loader-circle' : c.source && c.source !== 'web' ? sourceIcon[c.source] : 'i-lucide-messages-square'"
              class="size-4 shrink-0 text-(--ui-text-muted)" :class="{ 'animate-spin text-primary': c.active_turn }"
            />
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium">{{ c.title || t('chat.newThreadTitle') }}</p>
              <p class="truncate text-xs text-(--ui-text-muted)">{{ chatData?.projects[c.project_id] ?? c.project_id }} · {{ c.agent_name }} · {{ when(c.updated_at) }}</p>
            </div>
          </NuxtLink>
        </div>
      </UCard>
    </div>
  </PageShell>
</template>
