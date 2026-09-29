<script setup lang="ts">
const { user, isAdmin } = useAuth()
const { t, dateLocale } = useLang()

const { data: health } = await useFetch<{ status: string, version: string }>('/api/health')
const { data: prov } = await useFetch<{ providers: Provider[] }>('/api/providers')
const { data: proj } = await useFetch<{ projects: Project[] }>('/api/projects')
const { data: tpl } = await useFetch<{ templates: OrgModel[] }>('/api/templates')

interface Job { id: string, project_id: string, title: string, goal: string, pending_patches?: number, status: 'running' | 'done' | 'failed' | 'cancelled' | 'rejected' | 'needs_input', cost_usd: number, created_at: string }
const { data: jobData } = await useFetch<{ tasks: Job[], projects: Record<string, string> }>('/api/tasks/recent')
const recentJobs = computed(() => (jobData.value?.tasks ?? []).slice(0, 6))
const jobStatus = computed<Record<Job['status'], { label: string, color: 'info' | 'success' | 'error' | 'neutral' | 'warning', icon: string }>>(() => ({
  running: { label: t('home.jobRunning'), color: 'info', icon: 'i-lucide-loader' },
  done: { label: t('home.jobDone'), color: 'success', icon: 'i-lucide-circle-check' },
  failed: { label: t('home.jobFailed'), color: 'error', icon: 'i-lucide-circle-x' },
  cancelled: { label: t('home.jobCancelled'), color: 'neutral', icon: 'i-lucide-circle-slash' },
  rejected: { label: t('home.jobRejected'), color: 'warning', icon: 'i-lucide-thumbs-down' },
  needs_input: { label: t('home.jobNeedsInput'), color: 'warning', icon: 'i-lucide-message-circle-question' }
}))
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })

// what needs a person, and the last day in numbers
interface Incident { kind: string, severity: 'error' | 'warning', project_name: string, title: string, detail: string, link: string }
const { data: incData, refresh: refreshInc } = await useFetch<{ incidents: Incident[], count: number }>('/api/incidents')
let incTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => { incTimer = setInterval(() => refreshInc(), 30000) })
onBeforeUnmount(() => clearInterval(incTimer))
const incidents = computed(() => incData.value?.incidents ?? [])
// the kind of each, as a word next to it
const incKind = (k: string) => t(`incidents.kind.${k}` as 'incidents.kind.monitor')
const { data: stats } = await useFetch<{ totals: { running: number, pending: number, failed_24h: number, cost_24h: number } }>('/api/jobs/stats?since=24h')
const incIcon: Record<string, string> = {
  monitor: 'i-lucide-activity', process: 'i-lucide-square-terminal', automation: 'i-lucide-alarm-clock-off',
  bot: 'i-lucide-bot', jobs: 'i-lucide-circle-x', approval: 'i-lucide-stamp'
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
      <UCard>
        <div class="flex flex-wrap items-center justify-between gap-4">
          <div>
            <p class="text-sm text-(--ui-text-muted)">{{ t('home.welcome') }}</p>
            <p class="text-lg font-semibold">{{ user?.name || user?.email }}</p>
          </div>
          <div class="flex items-center gap-2">
            <UBadge :label="user?.role" :color="isAdmin ? 'primary' : 'neutral'" variant="subtle" />
            <UBadge v-if="health?.status === 'ok'" :label="t('home.apiConnected', { version: health.version })" color="success" variant="subtle" icon="i-lucide-plug" />
            <UBadge v-else :label="t('home.apiDisconnected')" color="error" variant="subtle" icon="i-lucide-plug-zap" />
          </div>
        </div>
      </UCard>

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
          <NuxtLink v-for="(x, i) in incidents" :key="i" :to="x.link" class="flex items-center gap-3 px-4 py-2.5 hover:bg-(--ui-bg-elevated)">
            <UIcon :name="incIcon[x.kind] ?? 'i-lucide-circle-alert'" class="size-4 shrink-0" :class="x.severity === 'error' ? 'text-(--ui-error)' : 'text-(--ui-warning)'" />
            <span class="min-w-0 flex-1">
              <span class="flex items-center gap-2">
                <span class="truncate text-sm font-medium">{{ x.title }}</span>
                <UBadge :label="incKind(x.kind)" color="neutral" variant="subtle" size="sm" />
              </span>
              <span class="block truncate text-xs text-(--ui-text-muted)">{{ x.project_name }}<template v-if="x.detail"> · {{ x.detail }}</template></span>
            </span>
            <UIcon name="i-lucide-chevron-right" class="size-4 shrink-0 text-(--ui-text-dimmed)" />
          </NuxtLink>
        </div>
      </UCard>

      <!-- the last day -->
      <div class="grid gap-3 sm:grid-cols-4">
        <UCard :ui="{ body: 'p-3 sm:p-4' }">
          <p class="text-xs text-(--ui-text-muted)">{{ t('job.running') }}</p>
          <p class="text-2xl font-semibold tabular-nums">{{ (stats?.totals.running ?? 0) + (stats?.totals.pending ?? 0) }}</p>
        </UCard>
        <UCard :ui="{ body: 'p-3 sm:p-4' }">
          <p class="text-xs text-(--ui-text-muted)">{{ t('job.failed24') }}</p>
          <p class="text-2xl font-semibold tabular-nums" :class="stats?.totals.failed_24h ? 'text-(--ui-error)' : ''">{{ stats?.totals.failed_24h ?? 0 }}</p>
        </UCard>
        <UCard :ui="{ body: 'p-3 sm:p-4' }">
          <p class="text-xs text-(--ui-text-muted)">{{ t('job.cost24') }}</p>
          <p class="text-2xl font-semibold tabular-nums">${{ (stats?.totals.cost_24h ?? 0).toFixed(2) }}</p>
        </UCard>
        <NuxtLink to="/projects">
          <UCard :ui="{ body: 'p-3 sm:p-4' }" class="h-full hover:bg-(--ui-bg-elevated)/40">
            <p class="text-xs text-(--ui-text-muted)">{{ t('home.project') }}</p>
            <p class="text-2xl font-semibold tabular-nums">{{ projects.length }}</p>
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

      <UCard v-if="recentJobs.length" :ui="{ body: 'p-0 sm:p-0' }">
        <template #header>
          <p class="font-semibold">{{ t('home.recentJobs') }}</p>
        </template>
        <div class="divide-y divide-(--ui-border)">
          <NuxtLink
            v-for="j in recentJobs" :key="j.id" :to="`/projects/${j.project_id}?tab=tasks&task=${j.id}`"
            class="flex items-center gap-3 px-4 py-2.5 transition hover:bg-(--ui-bg-elevated)"
          >
            <UIcon :name="jobStatus[j.status].icon" class="size-4 shrink-0" :class="{ 'animate-spin': j.status === 'running' }" />
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium">{{ j.title || j.goal }}</p>
              <p class="truncate text-xs text-(--ui-text-muted)">{{ jobData?.projects[j.project_id] ?? j.project_id }} · {{ when(j.created_at) }}</p>
            </div>
            <span class="text-xs tabular-nums text-(--ui-text-muted)">${{ j.cost_usd.toFixed(3) }}</span>
            <UBadge
              :color="j.status === 'done' && j.pending_patches ? 'warning' : jobStatus[j.status].color" variant="subtle" size="sm"
              :label="j.status === 'done' && j.pending_patches ? t('home.pendingApproval', { n: j.pending_patches }) : jobStatus[j.status].label"
            />
          </NuxtLink>
        </div>
      </UCard>
    </div>
  </PageShell>
</template>
