<script setup lang="ts">
// One agent of the project, shown by the box's room: a badge (its avatar in
// a halo: turning while it works, amber when it waits for you, red when its
// last job failed), a strip (what it is doing now), a card (and its day: jobs,
// cost, a string of beads for its last jobs) and a panel (and those jobs).
// A click opens the chat it works in (or the last one).
const { t, when } = useLang()
const { projectId, config, shape, set } = useWatchBox()

const { data: agentsData } = useLiveFetch<{ agents: Agent[], default_agent_id: string }>(() => `/api/projects/${projectId.value}/chat/agents`, { lazy: true, server: false })
const agent = computed(() => {
  const list = agentsData.value?.agents ?? []
  const id = String(config.value.agentId || '') || agentsData.value?.default_agent_id
  return list.find(a => a.id === id) ?? null
})
const query = computed(() => ({ project: projectId.value, agent: agent.value?.id ?? '', limit: 30 }))
const { data, error } = useLiveFetch<{ jobs: Job[] }>('/api/jobs', { query, lazy: true, server: false })
const jobs = computed(() => agent.value ? (data.value?.jobs ?? []).filter(j => j.agent_id === agent.value!.id) : [])

type State = 'running' | 'waiting' | 'needs_input' | 'failed' | 'idle'
const current = computed(() => jobs.value.find(j => j.status === 'running') ?? jobs.value.find(j => j.status === 'needs_input') ?? jobs.value.find(j => j.status === 'pending'))
const last = computed(() => jobs.value.find(j => !['running', 'pending', 'needs_input'].includes(j.status)))
const state = computed<State>(() => {
  const c = current.value
  if (c) return c.status === 'running' ? 'running' : c.status === 'needs_input' ? 'needs_input' : 'waiting'
  return last.value?.status === 'failed' ? 'failed' : 'idle'
})
const stateText = computed(() => {
  const c = current.value
  if (c) return `${t(`watchW.agent.state.${state.value}` as 'watchW.agent.state.running')}: ${c.title}`
  if (state.value === 'failed') return `${t('watchW.agent.state.failed')} · ${when(last.value!.created_at)}`
  return last.value ? `${t('watchW.agent.state.idle')} · ${when(last.value.finished_at ?? last.value.created_at)}` : t('watchW.agent.state.idle')
})
const halo: Record<State, string> = {
  running: 'bg-[conic-gradient(var(--ui-primary),transparent_55%,var(--ui-primary))] animate-spin [animation-duration:2.4s]',
  waiting: 'bg-(--ui-text-dimmed)/40',
  needs_input: 'bg-(--ui-warning) animate-pulse',
  failed: 'bg-(--ui-error)',
  idle: 'bg-(--ui-border)'
}
const dot: Record<string, string> = {
  running: 'bg-(--ui-info)', pending: 'bg-(--ui-text-dimmed)', needs_input: 'bg-(--ui-warning)', done: 'bg-(--ui-success)',
  failed: 'bg-(--ui-error)', cancelled: 'bg-(--ui-border-accented)', skipped: 'bg-(--ui-border-accented)'
}
// the day: jobs and cost in the last 24 hours
const day = computed(() => {
  const since = Date.now() - 86400000
  const list = jobs.value.filter(j => Date.parse(j.created_at) >= since)
  return { n: list.length, cost: list.reduce((a, j) => a + (j.cost_usd || 0), 0) }
})
const beads = computed(() => jobs.value.slice(0, shape.value === 'panel' ? 24 : 12).reverse())
const chatLink = computed(() => {
  const c = (current.value ?? jobs.value[0])?.conversation_id
  return c ? `/projects/${projectId.value}?tab=chat&c=${c}` : `/projects/${projectId.value}?tab=chat`
})
const money = (v: number) => `$${v.toFixed(2)}`
const toneClass: Record<string, string> = { info: 'text-(--ui-info)', neutral: 'text-(--ui-text-dimmed)', success: 'text-(--ui-success)', error: 'text-(--ui-error)', warning: 'text-(--ui-warning)' }
</script>

<template>
  <WatchEmpty v-if="agentsData && !agent" icon="i-lucide-bot" :text="t('watchW.agent.none')" />
  <div v-else-if="!agent" class="grid h-full place-items-center"><USkeleton class="size-10 rounded-full" /></div>
  <NuxtLink
    v-else :to="chatLink" :title="`${agent.name} · ${stateText}`"
    class="flex h-full min-h-0 overflow-hidden transition hover:bg-(--ui-bg-elevated)/50"
    :class="shape === 'strip' ? 'items-center gap-2.5 px-2.5' : shape === 'panel' ? 'flex-col gap-2 p-3' : 'flex-col items-center justify-center gap-1 p-1.5 text-center'"
  >
    <!-- the avatar in its halo, and who it is -->
    <div class="flex min-w-0 items-center gap-2.5" :class="shape === 'panel' ? 'w-full' : shape === 'strip' ? 'contents' : 'flex-col gap-1'">
      <span class="relative grid shrink-0 place-items-center rounded-full p-[3px]">
        <span class="absolute inset-0 rounded-full" :class="halo[state]" />
        <AgentAvatar :agent="agent" :size="shape === 'card' ? 'lg' : 'md'" class="relative ring-2 ring-(--ui-bg)" />
      </span>
      <span class="min-w-0" :class="shape === 'strip' || shape === 'panel' ? 'flex-1 text-start' : 'w-full'">
        <span class="block truncate font-medium" :class="shape === 'badge' ? 'text-[11px]' : 'text-sm'">{{ agent.name }}</span>
        <span v-if="shape !== 'badge'" class="block truncate text-xs" :class="state === 'failed' ? 'text-(--ui-error)' : state === 'needs_input' ? 'text-(--ui-warning)' : 'text-(--ui-text-muted)'">{{ stateText }}</span>
      </span>
      <span v-if="shape === 'panel'" class="shrink-0 text-end text-xs text-(--ui-text-muted) tabular-nums">
        <span class="block font-semibold text-(--ui-text)">{{ money(day.cost) }}</span>{{ t('watchW.agent.jobs', { n: day.n }) }}
      </span>
      <span v-if="shape !== 'badge'" class="shrink-0" @click.stop>
        <AgentPicker :project-id="projectId" :model-value="String(config.agentId || '')" :selected-agent="agent" compact icon-only @update:model-value="v => set('agentId', v)" />
      </span>
    </div>
    <!-- its day, and its last jobs as beads (oldest left) -->
    <template v-if="shape === 'card' || shape === 'column' || shape === 'panel'">
      <p v-if="shape !== 'panel'" class="text-[11px] text-(--ui-text-muted) tabular-nums">{{ t('watchW.agent.today', { n: day.n, cost: money(day.cost) }) }}</p>
      <div v-if="beads.length" class="flex flex-wrap gap-1" :class="shape === 'panel' ? '' : 'justify-center'">
        <span v-for="j in beads" :key="j.id" class="size-2 rounded-full" :class="[dot[j.status], j.status === 'running' && 'animate-pulse']" :title="`${j.title} · ${t(`job.status.${j.status}`)}`" />
      </div>
    </template>
    <!-- a panel: the jobs themselves -->
    <ul v-if="shape === 'panel'" class="min-h-0 w-full flex-1 divide-y divide-(--ui-border) overflow-y-auto">
      <li v-if="error" class="py-1 text-xs text-(--ui-error)">{{ apiError(error) }}</li>
      <li v-for="j in jobs.slice(0, 12)" :key="j.id" class="flex items-center gap-2 py-1 text-xs">
        <UIcon :name="jobStatusMeta[j.status].icon" class="size-3.5 shrink-0" :class="[toneClass[jobStatusMeta[j.status].color], j.status === 'running' && 'animate-spin']" />
        <span class="min-w-0 flex-1 truncate">{{ j.title }}</span>
        <span v-if="j.cost_usd" class="shrink-0 text-(--ui-text-muted) tabular-nums">{{ money(j.cost_usd) }}</span>
        <span class="shrink-0 text-(--ui-text-dimmed) tabular-nums">{{ when(j.created_at) }}</span>
      </li>
    </ul>
  </NuxtLink>
</template>
