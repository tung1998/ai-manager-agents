<script setup lang="ts">
// The Job page: one row per piece of work (a chat, a task, an automation's
// runs) with how its jobs went, not one per answer. A row opens that work.
const props = defineProps<{ project?: string, projects?: { id: string, name: string }[] }>()
const toast = useToast()
const { t, dateLocale } = useLang()

interface Group {
  key: string, kind: 'chat' | 'task' | 'automation' | 'job', project_id: string, project_name: string, source: Source,
  title: string, status: string, runs: number, failed: number, active: number, cost_usd: number, last_at: string, link: string, job_id?: string
}

const origin = ref<'all' | Source>('all')
const search = ref('')
const searchQ = ref('')
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(search, (v) => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { searchQ.value = v.trim() }, 300)
})
const status = ref('')
const project = ref(props.project ?? '')
const since = ref('168h')
const groups = ref<Group[]>([])
const nextBefore = ref('')
const loading = ref(false)

function query(before = '') {
  const q = new URLSearchParams()
  const set = (k: string, v?: string) => { if (v) q.set(k, v) }
  set('project', project.value)
  set('source', origin.value === 'all' ? '' : origin.value)
  set('q', searchQ.value)
  set('status', status.value)
  set('since', since.value)
  set('before', before)
  set('limit', '20')
  return q.toString()
}
async function load(more = false) {
  loading.value = true
  try {
    const res = await $fetch<{ groups: Group[], next_before: string }>(`/api/jobs/groups?${query(more ? nextBefore.value : '')}`)
    groups.value = more ? [...groups.value, ...res.groups] : res.groups
    nextBefore.value = res.next_before
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    loading.value = false
  }
}
watch([origin, searchQ, status, project, since], () => load(), { immediate: true })

// follow what is still running
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  timer = setInterval(() => {
    if (groups.value.some(g => g.active > 0)) load()
  }, 5000)
})
onBeforeUnmount(() => clearInterval(timer))

const ALL = '__all'
const bind = (r: Ref<string>) => computed({ get: () => r.value || ALL, set: (v: string) => { r.value = v === ALL ? '' : v } })
const statusSel = bind(status)
const projectSel = bind(project)
const selectItems = computed(() => ({
  status: [{ label: t('job.statusAll'), value: ALL }, ...(['running', 'pending', 'done', 'failed', 'needs_input', 'cancelled', 'skipped'] as const).map(v => ({ label: t(`job.status.${v}`), value: v }))],
  since: [{ label: t('job.range24h'), value: '24h' }, { label: t('job.range7d'), value: '168h' }, { label: t('job.range30d'), value: '720h' }],
  project: [{ label: t('job.projectAll'), value: ALL }, ...(props.projects ?? []).map(p => ({ label: p.name, value: p.id }))]
}))
const activeFilters = computed(() => [origin.value !== 'all', !!status.value, !!project.value && !props.project, since.value !== '168h'].filter(Boolean).length)

const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
const usd = (v: number) => v >= 1 ? `$${v.toFixed(2)}` : v > 0 ? `$${v.toFixed(3)}` : ''
const kindIcon = (g: Group) => g.active ? 'i-lucide-loader-circle'
  : g.source !== 'web' ? sourceIcon[g.source]
    : ({ chat: 'i-lucide-messages-square', task: 'i-lucide-list-todo', automation: 'i-lucide-alarm-clock', job: 'i-lucide-square-terminal' })[g.kind]
const detail = ref<string | null>(null) // a job of its own, shown in place
function open(g: Group) {
  if (g.job_id) detail.value = g.job_id
  else navigateTo(g.link)
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center gap-2">
      <UInput v-model="search" size="sm" icon="i-lucide-search" class="min-w-0 flex-1 sm:max-w-72" :placeholder="t('job.search')" />
      <FilterButton :active="activeFilters">
        <SourceFilter v-model="origin" />
        <USelect v-model="statusSel" size="sm" :items="selectItems.status" class="w-full" />
        <USelect v-if="projects?.length" v-model="projectSel" size="sm" :items="selectItems.project" class="w-full" />
        <USelect v-model="since" size="sm" :items="selectItems.since" class="w-full" />
      </FilterButton>
    </div>

    <UCard :ui="{ body: 'p-0 sm:p-0' }">
      <p v-if="!groups.length && !loading" class="p-4 text-sm text-(--ui-text-muted)">{{ t('job.empty') }}</p>
      <div class="divide-y divide-(--ui-border)">
        <button
          v-for="g in groups" :key="g.key" type="button"
          class="flex w-full items-center gap-3 px-4 py-2.5 text-left transition hover:bg-(--ui-bg-elevated)" @click="open(g)"
        >
          <UIcon :name="kindIcon(g)" class="size-4 shrink-0 text-(--ui-text-muted)" :class="{ 'animate-spin text-primary': g.active }" />
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-medium">{{ g.title || t('chat.newThreadTitle') }}</p>
            <p class="truncate text-xs text-(--ui-text-muted)">
              {{ g.project_name }} · {{ t('job.groupRuns', { n: g.runs }) }}<template v-if="usd(g.cost_usd)"> · {{ usd(g.cost_usd) }}</template> · {{ when(g.last_at) }}
            </p>
          </div>
          <UBadge v-if="g.failed" :label="t('job.groupFailed', { n: g.failed })" color="error" variant="subtle" size="sm" class="shrink-0" />
        </button>
      </div>
      <div v-if="nextBefore" class="border-t border-(--ui-border) p-2 text-center">
        <UButton size="xs" color="neutral" variant="ghost" :loading="loading" :label="t('job.more')" @click="load(true)" />
      </div>
    </UCard>
    <JobDetailModal :job-id="detail" @close="detail = null" />
  </div>
</template>
