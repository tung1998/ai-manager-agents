<script setup lang="ts">
// What needs a person across the office, each with where to fix it: monitors
// down, processes crashed, automations office turned off, bots cut off,
// failed runs, cards waiting for approval.
interface Incident { kind: 'monitor' | 'process' | 'automation' | 'bot' | 'jobs' | 'approval', severity: 'error' | 'warning', project_id: string, project_name: string, title: string, detail: string, at: string, link: string }
const { t, dateLocale } = useLang()
const { data, refresh, status } = await useFetch<{ incidents: Incident[], count: number }>('/api/incidents')
const list = computed(() => data.value?.incidents ?? [])
const icon: Record<Incident['kind'], string> = {
  monitor: 'i-lucide-activity', process: 'i-lucide-square-terminal', automation: 'i-lucide-alarm-clock-off',
  bot: 'i-lucide-bot', jobs: 'i-lucide-circle-x', approval: 'i-lucide-stamp'
}
const when = (d: string) => d && !d.startsWith('0001') ? new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' }) : ''
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { timer = setInterval(() => refresh(), 30000) })
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <PageShell :title="t('incidents.title')">
    <template #actions>
      <UButton size="sm" color="neutral" variant="outline" icon="i-lucide-refresh-cw" :loading="status === 'pending'" :label="t('incidents.refresh')" @click="refresh()" />
    </template>
    <div class="max-w-4xl space-y-3">
      <p class="text-sm text-(--ui-text-muted)">{{ t('incidents.desc') }}</p>
      <UCard v-if="!list.length" :ui="{ body: 'p-6' }">
        <p class="flex items-center gap-2 text-sm"><UIcon name="i-lucide-circle-check" class="size-5 text-(--ui-success)" />{{ t('incidents.none') }}</p>
      </UCard>
      <UCard v-else :ui="{ body: 'p-0 sm:p-0' }">
        <NuxtLink
          v-for="(x, i) in list" :key="i" :to="x.link"
          class="flex items-start gap-3 border-b border-(--ui-border) px-4 py-3 last:border-0 hover:bg-(--ui-bg-elevated)/50"
        >
          <UIcon :name="icon[x.kind]" class="mt-0.5 size-5 shrink-0" :class="x.severity === 'error' ? 'text-(--ui-error)' : 'text-(--ui-warning)'" />
          <span class="min-w-0 flex-1">
            <span class="flex flex-wrap items-center gap-x-2">
              <span class="font-medium">{{ x.title }}</span>
              <UBadge :label="t(`incidents.kind.${x.kind}`)" color="neutral" variant="subtle" size="sm" />
              <span class="text-xs text-(--ui-text-muted)">{{ x.project_name }}</span>
            </span>
            <span v-if="x.detail" class="line-clamp-2 block text-xs text-(--ui-text-muted)">{{ x.detail }}</span>
          </span>
          <span class="shrink-0 text-xs text-(--ui-text-dimmed)">{{ when(x.at) }}</span>
          <UIcon name="i-lucide-chevron-right" class="mt-0.5 size-4 shrink-0 text-(--ui-text-dimmed)" />
        </NuxtLink>
      </UCard>
    </div>
  </PageShell>
</template>
