<script setup lang="ts">
// The few figures worth a glance on every page (admin): what waits for a
// person, the agents at work, the machine's CPU and memory. Each opens its
// tab of the overview.
interface Summary { cpu_percent: number, mem_used: number, mem_total: number, procs: number, agents: number }
const { t } = useLang()
const { data: inc } = useLiveFetch<{ count: number }>('/api/incidents', { key: 'incidents', lazy: true })

// shared by every page's header: one look every 5 seconds, while the tab shows
const sum = useState<Summary | null>('header-stats', () => null)
async function load() {
  if (document.visibilityState !== 'visible') return
  try {
    sum.value = await $fetch<Summary>('/api/system/summary')
  } catch { /* the next look tries again */ }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { void load(); timer = setInterval(load, 5000) })
onBeforeUnmount(() => clearInterval(timer))

const memPct = computed(() => sum.value?.mem_total ? Math.round(sum.value.mem_used / sum.value.mem_total * 100) : 0)
const hot = (p: number) => p >= 90 ? 'text-(--ui-error)' : p >= 75 ? 'text-(--ui-warning)' : ''
const items = computed(() => {
  const s = sum.value
  const n = inc.value?.count ?? 0
  return [
    { key: 'inc', icon: 'i-lucide-siren', value: String(n), title: t('hdr.incidents', { n }), to: '/', cls: n ? 'text-(--ui-error)' : '', mobile: true },
    { key: 'agents', icon: 'i-lucide-bot', value: s ? String(s.agents) : '–', title: s ? t('hdr.agents', { n: s.agents, p: s.procs }) : '', to: '/?tab=machine', cls: s?.agents ? 'text-primary' : '', mobile: true },
    { key: 'cpu', icon: 'i-lucide-cpu', value: s ? `${Math.round(s.cpu_percent)}%` : '–', title: t('hdr.cpu'), to: '/?tab=machine', cls: s ? hot(s.cpu_percent) : '', mobile: false },
    { key: 'ram', icon: 'i-lucide-memory-stick', value: s ? `${memPct.value}%` : '–', title: t('hdr.ram'), to: '/?tab=machine', cls: hot(memPct.value), mobile: false }
  ]
})
</script>

<template>
  <div class="flex items-center gap-0.5">
    <NuxtLink
      v-for="x in items" :key="x.key" :to="x.to" :title="x.title" :aria-label="x.title"
      class="items-center gap-1 rounded-md px-1.5 py-1 text-xs tabular-nums transition hover:bg-(--ui-bg-elevated)"
      :class="[x.mobile ? 'flex' : 'hidden sm:flex', x.cls || 'text-(--ui-text-muted) hover:text-(--ui-text)']"
    >
      <UIcon :name="x.icon" class="size-3.5" />{{ x.value }}
    </NuxtLink>
  </div>
</template>
