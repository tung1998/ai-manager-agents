<script setup lang="ts">
// A project's workflows (ADR-098): its own copies of the library's, each role
// filled by one of its agents; a chat runs one with /key.
import type { MessageKey } from '~/locales/vi'

const props = defineProps<{ projectId: string }>()
const toast = useToast()
const { t, dateLocale } = useLang()
const { isAdmin } = useAuth()

const { data, refresh, pending, error: loadError } = useLiveFetch<{ workflows: ProjectWorkflow[], library: LibraryWorkflow[] }>(() => `/api/projects/${props.projectId}/workflows`, { lazy: true })
const { data: agentsData } = useLiveFetch<{ agents: Agent[] }>(() => `/api/projects/${props.projectId}/chat/agents`, { lazy: true })
const workflows = computed(() => data.value?.workflows ?? [])

// the library's not installed yet (by the key it was copied from)
const installable = computed(() => {
  const have = new Set(workflows.value.flatMap(w => [w.key, w.source_key]))
  return (data.value?.library ?? []).filter(l => !have.has(l.def.key))
})
const installing = ref(false)
const installItems = computed(() => [installable.value.length
  ? installable.value.map(l => ({ label: `${l.def.name} · /${l.def.key}`, icon: 'i-lucide-workflow', disabled: !!l.error, onSelect: () => install(l.def.key) }))
  : [{ label: t('wf.allInstalled'), disabled: true }]])

// a role's agent: "_" leaves it to the coordinator (a select item needs a value)
const NONE = '_'
const agentItems = computed(() => [
  { label: t('wf.unbound'), value: NONE },
  ...(agentsData.value?.agents ?? []).filter(a => a.enabled !== false).map(a => ({ label: a.name, value: a.id }))
])

const busy = ref('') // the workflow being changed
async function call(w: ProjectWorkflow | null, fn: () => Promise<unknown>, done?: string) {
  busy.value = w?.id ?? '*'
  try {
    await fn()
    if (done) toast.add({ title: done, color: 'success' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = ''
  }
}
const patch = (w: ProjectWorkflow, body: object) => call(w, () => $fetch(`/api/workflows/${w.id}`, { method: 'PATCH', body }))
function bind(w: ProjectWorkflow, role: string, agent: string) {
  const bindings = { ...w.bindings }
  if (agent === NONE) delete bindings[role]
  else bindings[role] = agent
  return patch(w, { bindings })
}
async function install(key: string) {
  installing.value = true
  await call(null, () => $fetch(`/api/projects/${props.projectId}/workflows`, { method: 'POST', body: { key } }), t('wf.installed'))
  installing.value = false
}
function remove(w: ProjectWorkflow) {
  if (!confirm(t('wf.deleteConfirm', { name: w.name }))) return
  return call(w, () => $fetch(`/api/workflows/${w.id}`, { method: 'DELETE' }))
}
function toLibrary(w: ProjectWorkflow) {
  const there = (data.value?.library ?? []).some(l => l.def.key === w.key)
  if (there && !confirm(t('wf.overwrite', { key: w.key }))) return
  return call(w, () => $fetch(`/api/workflows/${w.id}/to-library`, { method: 'POST', body: {} }), t('wf.savedToLibrary'))
}
const fromLibrary = (w: ProjectWorkflow) => call(w, () => $fetch(`/api/workflows/${w.id}/from-library`, { method: 'POST', body: {} }), t('wf.updated'))

const editTo = (w?: ProjectWorkflow) => ({ path: `/projects/${props.projectId}/workflows/edit`, query: w ? { w: w.id } : {} })
function menu(w: ProjectWorkflow) {
  return [[
    { label: t('wf.edit'), icon: 'i-lucide-pencil', to: editTo(w) },
    ...(w.has_update ? [{ label: t('wf.fromLibrary'), icon: 'i-lucide-download', onSelect: () => fromLibrary(w) }] : []),
    { label: t('wf.toLibrary'), icon: 'i-lucide-library', disabled: !!w.error, onSelect: () => toLibrary(w) }
  ], [{ label: t('wf.delete'), icon: 'i-lucide-trash-2', color: 'error' as const, onSelect: () => remove(w) }]]
}

const statusColor: Record<WorkflowRun['status'], 'info' | 'success' | 'error' | 'neutral'> = { running: 'info', done: 'success', failed: 'error', stopped: 'neutral' }
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })
const runLink = (r: WorkflowRun) => ({ path: `/projects/${props.projectId}`, query: { tab: 'chat', c: r.conversation_id } })
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-2">
      <p class="text-sm text-(--ui-text-muted)">{{ t('wf.count', { n: workflows.length }) }}</p>
      <UTooltip :text="t('wf.projectInfo')"><UIcon name="i-lucide-info" class="size-4 text-(--ui-text-muted)" /></UTooltip>
      <div v-if="isAdmin" class="ms-auto flex gap-2">
        <UDropdownMenu :items="installItems" :content="{ align: 'end' }" :ui="{ content: 'max-h-80' }">
          <UButton size="sm" color="neutral" variant="outline" icon="i-lucide-download" :label="t('wf.install')" :loading="installing" />
        </UDropdownMenu>
        <UButton size="sm" icon="i-lucide-plus" :label="t('wf.new')" :to="editTo()" />
      </div>
    </div>

    <UAlert v-if="loadError" color="error" variant="subtle" :title="apiError(loadError)" />
    <LoadingRows v-else-if="pending && !data" />
    <div v-else-if="!workflows.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
      <UIcon name="i-lucide-workflow" class="mx-auto size-8 text-(--ui-text-dimmed)" />
      <p class="mt-2 font-medium">{{ t('wf.empty') }}</p>
      <p class="text-sm text-(--ui-text-muted)">{{ t('wf.emptyHint') }}</p>
    </div>

    <div v-for="w in workflows" :key="w.id" class="space-y-3 rounded-lg border border-(--ui-border) p-4" :class="!w.enabled && 'opacity-70'">
      <div class="flex flex-wrap items-start gap-x-3 gap-y-1">
        <UIcon name="i-lucide-workflow" class="mt-0.5 size-5 shrink-0 text-primary" />
        <div class="min-w-0 flex-1">
          <p class="flex flex-wrap items-center gap-2">
            <NuxtLink :to="editTo(w)" class="font-semibold hover:text-primary">{{ w.name }}</NuxtLink>
            <span class="font-mono text-xs text-(--ui-text-muted)">/{{ w.key }}</span>
            <UBadge v-if="w.error" color="error" variant="subtle" size="sm" icon="i-lucide-circle-alert" :label="t('wf.invalid')" :title="w.error" />
            <UButton v-if="w.has_update && isAdmin" size="xs" color="primary" variant="soft" icon="i-lucide-download" :label="t('wf.fromLibrary')" :loading="busy === w.id" @click="fromLibrary(w)" />
          </p>
          <p v-if="w.description" class="line-clamp-2 text-sm text-(--ui-text-muted)">{{ w.description }}</p>
          <p v-if="w.error" class="mt-1 text-xs text-(--ui-error)">{{ w.error }}</p>
        </div>
        <USwitch :model-value="w.enabled" :disabled="!isAdmin || busy === w.id" :aria-label="t('wf.enabled')" :title="t('wf.enabled')" @update:model-value="(v: boolean) => patch(w, { enabled: v })" />
        <UDropdownMenu v-if="isAdmin" :items="menu(w)" :content="{ align: 'end' }">
          <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-ellipsis-vertical" :aria-label="t('chat.more')" />
        </UDropdownMenu>
      </div>

      <div v-if="w.roles.length" class="grid gap-2 sm:grid-cols-2">
        <div v-for="r in w.roles" :key="r.key" class="flex items-center gap-2 text-sm">
          <UTooltip :text="t(`wf.access.${r.access}` as MessageKey)">
            <UIcon :name="accessIcon[r.access] ?? 'i-lucide-eye'" class="size-4 shrink-0 text-(--ui-text-muted)" />
          </UTooltip>
          <span class="min-w-0 flex-1 truncate" :title="r.hint">{{ r.name }}</span>
          <USelect
            :model-value="w.bindings[r.key] || NONE" :items="agentItems" size="sm" class="w-44 shrink-0" :disabled="!isAdmin || busy === w.id"
            :aria-label="t('wf.roleAgent', { role: r.name })" @update:model-value="(v: string) => bind(w, r.key, v)"
          />
        </div>
      </div>

      <p v-if="w.last_run" class="flex flex-wrap items-center gap-2 text-xs text-(--ui-text-muted)">
        <span>{{ t('wf.lastRun') }}</span>
        <UBadge :color="statusColor[w.last_run.status]" variant="subtle" size="sm" :label="t(`wf.status.${w.last_run.status}` as MessageKey)" />
        <NuxtLink :to="runLink(w.last_run)" class="hover:text-primary">{{ when(w.last_run.started_at) }}</NuxtLink>
        <span>· ${{ w.last_run.cost_usd.toFixed(3) }}</span>
      </p>
    </div>
  </div>
</template>
