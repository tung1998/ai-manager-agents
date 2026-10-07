<script setup lang="ts">
// A project's Burn review profiles (ADR-113): saved review setups, each stage
// (problem, plan, result) with its own reviewer agent and workflow. The
// Burn's settings pick one.
interface AgentLite { id: string, name: string, enabled?: boolean }

const route = useRoute()
const { t } = useLang()
const toast = useToast()
const projectId = computed(() => String(route.params.id))
const base = computed(() => `/api/projects/${projectId.value}/burn/review-profiles`)
const { data, error, refresh } = useLiveFetch<{ profiles: BurnReviewProfile[] }>(base)
const { data: agentsData } = useLiveFetch<{ agents: AgentLite[] }>(() => `/api/projects/${projectId.value}/chat/agents`, { lazy: true })
const { data: wfData } = useLiveFetch<{ workflows: ProjectWorkflow[] }>(() => `/api/projects/${projectId.value}/workflows`, { lazy: true })
const profiles = computed(() => data.value?.profiles ?? [])
useHead({ title: () => t('burn.review.profiles') })

const agentItems = computed(() => [{ value: '__burn', label: t('burn.review.agentSame') },
  ...(agentsData.value?.agents ?? []).map(a => ({ value: a.id, label: a.enabled === false ? `${a.name} (${t('burn.agentOffTag')})` : a.name }))])
const workflowItems = computed(() => [{ value: '__none', label: t('burn.review.workflowNone') },
  ...(wfData.value?.workflows ?? []).filter(w => w.enabled && !w.error && w.callable !== 'sub').map(w => ({ value: w.key, label: `#${w.key} · ${w.name}` }))])

// the profile being edited ('' = a new one)
const selected = ref<string | null>(null)
interface StageForm { on: boolean, agent: string, workflow: string }
const blank = (): StageForm => ({ on: false, agent: '__burn', workflow: '__none' })
const form = reactive({ name: '', stages: { issue: blank(), plan: blank(), result: blank() } as Record<ReviewStage, StageForm> })
function load(p?: BurnReviewProfile) {
  selected.value = p?.id ?? ''
  form.name = p?.name ?? ''
  for (const k of reviewStageKeys) {
    const st = p?.stages[k]
    form.stages[k] = { on: !!st, agent: st?.agent_id || '__burn', workflow: st?.workflow || '__none' }
  }
}
watch(data, (d) => { if (d && selected.value === null) load(d.profiles[0]) }, { immediate: true }) // the first one, once loaded

const saving = ref(false)
async function save() {
  saving.value = true
  const stages: Partial<Record<ReviewStage, BurnReviewStage>> = {}
  for (const k of reviewStageKeys) {
    const st = form.stages[k]
    if (st.on) stages[k] = { agent_id: st.agent === '__burn' ? '' : st.agent, workflow: st.workflow === '__none' ? '' : st.workflow }
  }
  try {
    const body = { name: form.name, stages }
    const res = selected.value
      ? await $fetch<{ profile: BurnReviewProfile }>(`/api/burn-review-profiles/${selected.value}`, { method: 'PUT', body })
      : await $fetch<{ profile: BurnReviewProfile }>(base.value, { method: 'POST', body })
    await refresh()
    load(res.profile)
    toast.add({ title: t('burn.saved'), color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    saving.value = false
  }
}
async function remove() {
  const p = profiles.value.find(x => x.id === selected.value)
  if (!p || !confirm(t('burn.review.deleteConfirm', { name: p.name }))) return
  try {
    await $fetch(`/api/burn-review-profiles/${p.id}`, { method: 'DELETE' })
    selected.value = null
    await refresh()
    if (!profiles.value.length) load()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
</script>

<template>
  <PageShell :title="t('burn.review.profiles')">
    <div>
      <UButton :to="{ path: `/projects/${projectId}`, query: { tab: 'burn' } }" icon="i-lucide-arrow-left" size="xs" color="neutral" variant="ghost" class="-ms-2 mb-2" :label="t('burn.review.back')" />
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :title="apiError(error)" :actions="[{ label: t('common.refresh'), onClick: () => refresh() }]" />
    <LoadingRows v-else-if="!data" />
    <div v-else class="grid gap-4 md:grid-cols-[16rem_1fr]">
      <!-- the profiles -->
      <div class="space-y-1">
        <UButton block color="neutral" variant="outline" icon="i-lucide-plus" :label="t('burn.review.new')" @click="load()" />
        <button
          v-for="p in profiles" :key="p.id" type="button"
          class="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-start text-sm hover:bg-(--ui-bg-elevated)"
          :class="selected === p.id ? 'bg-(--ui-bg-elevated) font-medium' : ''"
          @click="load(p)"
        >
          <UIcon name="i-lucide-scan-eye" class="size-4 shrink-0 text-(--ui-text-muted)" />
          <span class="min-w-0 flex-1 truncate">{{ p.name }}</span>
          <UBadge v-if="p.in_use" :label="t('burn.review.inUse')" color="warning" variant="subtle" size="sm" />
        </button>
        <p v-if="!profiles.length" class="px-3 py-2 text-xs text-(--ui-text-dimmed)">{{ t('burn.review.empty') }}</p>
      </div>

      <!-- one profile -->
      <UCard :ui="{ body: 'space-y-4 sm:p-4' }">
        <UFormField :label="t('burn.review.name')" required>
          <UInput v-model="form.name" class="w-full" :placeholder="t('burn.review.namePlaceholder')" />
        </UFormField>
        <div v-for="k in reviewStageKeys" :key="k" class="space-y-3 rounded-lg border border-(--ui-border) p-3">
          <USwitch v-model="form.stages[k].on" :label="t('burn.review.stageTitle', { stage: t(`burn.review.${k}`) })" :description="t(`burn.review.${k}Help`)" />
          <div v-if="form.stages[k].on" class="grid gap-3 sm:grid-cols-2">
            <UFormField :label="t('burn.review.agent')">
              <USelect v-model="form.stages[k].agent" :items="agentItems" class="w-full" />
            </UFormField>
            <UFormField :label="t('burn.review.workflow')" :help="t('burn.review.workflowHelp')">
              <USelect v-model="form.stages[k].workflow" :items="workflowItems" class="w-full" />
            </UFormField>
          </div>
        </div>
        <div class="flex justify-end gap-2">
          <UButton v-if="selected" color="error" variant="ghost" icon="i-lucide-trash-2" :label="t('common.delete')" @click="remove" />
          <UButton :label="t('common.save')" :loading="saving" :disabled="!form.name.trim()" @click="save" />
        </div>
      </UCard>
    </div>
  </PageShell>
</template>
