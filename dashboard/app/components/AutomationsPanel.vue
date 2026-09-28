<script setup lang="ts">
// A project's automations: schedules and webhooks that start a chat turn or a
// task as a job (ADR-040).
const props = defineProps<{ projectId: string }>()
const toast = useToast()
const { isAdmin } = useAuth()
const { t, dateLocale } = useLang()

const { data, refresh } = await useFetch<{ automations: Automation[] }>(() => `/api/projects/${props.projectId}/automations`)
const { data: agentsData } = useFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
const list = computed(() => data.value?.automations ?? [])
const agentName = (id: string) => agentsData.value?.agents.find(a => a.id === id)?.name ?? t('auto.lead')
const when = (d?: string | null) => d ? new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' }) : t('auto.never')

const editorOpen = ref(false)
const editing = ref<Automation | null>(null)
function open(a: Automation | null) {
  editing.value = a
  editorOpen.value = true
}

async function toggle(a: Automation, enabled: boolean) {
  try {
    await $fetch(`/api/automations/${a.id}`, { method: 'PATCH', body: automationBody({ ...a, enabled }) })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
async function runNow(a: Automation) {
  try {
    await $fetch(`/api/automations/${a.id}/run`, { method: 'POST', body: {} })
    toast.add({ title: t('auto.ran'), color: 'success' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
</script>

<template>
  <div class="max-w-5xl space-y-3">
    <div class="flex items-center justify-between gap-2">
      <p class="text-sm text-(--ui-text-muted)">{{ list.length ? '' : t('auto.empty') }}</p>
      <UButton v-if="isAdmin" icon="i-lucide-plus" size="sm" :label="t('auto.new')" @click="open(null)" />
    </div>

    <UCard v-if="list.length" :ui="{ body: 'p-0 sm:p-0' }">
      <div v-for="a in list" :key="a.id" class="flex flex-wrap items-center gap-3 border-b border-(--ui-border) px-4 py-3 last:border-0">
        <UIcon :name="a.source === 'schedule' ? 'i-lucide-alarm-clock' : 'i-lucide-webhook'" class="size-5 shrink-0 text-(--ui-text-muted)" />
        <NuxtLink :to="`/projects/${projectId}/automations/${a.id}`" class="min-w-0 flex-1 hover:underline">
          <span class="block truncate font-medium">{{ a.name }}</span>
          <span class="block truncate text-xs text-(--ui-text-muted)">
            {{ scheduleText(a, t) }}
            · {{ a.action === 'task' ? t('auto.toTask') : a.action === 'script' ? t('auto.toScript') : t('auto.toChat', { agent: agentName(a.agent_id) }) }}
          </span>
        </NuxtLink>
        <UBadge v-if="a.disabled_code" color="error" variant="subtle" size="sm" icon="i-lucide-circle-alert" :label="t('auto.disabledBy', { reason: a.disabled_reason })" class="max-w-64 truncate" />
        <span v-else class="flex items-center gap-1.5 text-xs text-(--ui-text-muted)">
          <JobStatusBadge v-if="a.last_job" :status="a.last_job.status" />
          {{ when(a.last_job?.created_at) }}
        </span>
        <USwitch v-if="isAdmin" :model-value="a.enabled" size="sm" @update:model-value="(v: boolean) => toggle(a, v)" />
        <UButton v-if="isAdmin" size="xs" color="neutral" variant="ghost" icon="i-lucide-play" :aria-label="t('auto.runNow')" :title="t('auto.runNow')" @click="runNow(a)" />
      </div>
    </UCard>

    <AutomationEditor v-model:open="editorOpen" :project-id="projectId" :automation="editing" @saved="refresh()" />
  </div>
</template>
