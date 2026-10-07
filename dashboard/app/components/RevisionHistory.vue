<script setup lang="ts">
// A project's agents as they were before each change (ADR-099), with a way back.
interface Revision {
  id: string
  action: string
  actor: string
  agent_count: number
  created_at: string
}
interface Snapshot {
  default?: string // the default agent's key
  agents: { key: string, name: string, role?: string, llm_model?: string, model_tier: ModelTier }[]
}

const props = defineProps<{ projectId: string }>()
const emit = defineEmits<{ restored: [] }>()
const open = defineModel<boolean>('open', { default: false })

const toast = useToast()
const { isAdmin } = useAuth()
const { t, dateLocale } = useLang()
const revisions = ref<Revision[]>([])
const loading = ref(false)
const expanded = ref<Record<string, Snapshot | null>>({})

async function load() {
  loading.value = true
  try {
    revisions.value = (await $fetch<{ revisions: Revision[] }>(`/api/projects/${props.projectId}/revisions`)).revisions
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    loading.value = false
  }
}
watch(open, (v) => { if (v) load() })

async function toggle(r: Revision) {
  if (expanded.value[r.id] !== undefined) {
    const { [r.id]: _, ...rest } = expanded.value
    expanded.value = rest
    return
  }
  expanded.value = { ...expanded.value, [r.id]: null }
  try {
    const res = await $fetch<{ snapshot: Snapshot }>(`/api/revisions/${r.id}`)
    expanded.value = { ...expanded.value, [r.id]: res.snapshot }
  } catch (e) {
    const { [r.id]: _, ...rest } = expanded.value
    expanded.value = rest
    toast.add({ title: apiError(e), color: 'error' })
  }
}

const restoring = ref('')
async function restore(r: Revision) {
  if (!confirm(t('team.rev.restoreConfirm'))) return
  restoring.value = r.id
  try {
    await $fetch(`/api/revisions/${r.id}/restore`, { method: 'POST', body: {} })
    toast.add({ title: t('rev.restored'), color: 'success' })
    emit('restored')
    await load()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    restoring.value = ''
  }
}

// A revision is the state *before* the action it names
// (agent.update:<key>, agent.create:<key>, agent.delete:<key>, pack:<key>, restore:<id>, import).
function describe(action: string) {
  const i = action.indexOf(':')
  const kind = i < 0 ? action : action.slice(0, i)
  const arg = i < 0 ? '' : action.slice(i + 1)
  const map: Record<string, string> = {
    'agent.update': t('rev.action.agentUpdate', { arg }),
    'agent.create': t('rev.action.agentCreate', { arg }),
    'agent.delete': t('rev.action.agentDelete', { arg }),
    'pack': t('team.rev.pack', { arg }),
    'restore': t('rev.action.restore'),
    'import': t('rev.action.import')
  }
  return t('rev.beforeAction', { action: map[kind] ?? action })
}
const who = (a: string) => a.replace(/^human:/, '')
useLive(['revisions', 'agents'], () => { if (open.value) load() })
</script>

<template>
  <USlideover v-model:open="open" :title="t('rev.title')" :ui="{ content: 'max-w-lg' }">
    <template #body>
      <p class="mb-4 text-sm text-(--ui-text-muted)">
        {{ t('team.rev.intro') }}
      </p>
      <p v-if="loading && !revisions.length" class="text-sm text-(--ui-text-muted)">{{ t('rev.loading') }}</p>
      <p v-else-if="!revisions.length" class="text-sm text-(--ui-text-muted)">{{ t('rev.empty') }}</p>
      <ol class="space-y-2">
        <li v-for="r in revisions" :key="r.id" class="rounded-lg border border-(--ui-border) p-3">
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <p class="text-sm font-medium">{{ describe(r.action) }}</p>
              <p class="text-xs text-(--ui-text-muted)">
                {{ new Date(r.created_at).toLocaleString(dateLocale) }} · {{ who(r.actor) }} · {{ t('rev.agentCount', { n: r.agent_count }) }}
              </p>
            </div>
            <div class="flex shrink-0 gap-1">
              <UButton size="xs" color="neutral" variant="ghost" :label="expanded[r.id] !== undefined ? t('rev.hide') : t('rev.view')" @click="toggle(r)" />
              <UButton v-if="isAdmin" size="xs" variant="soft" icon="i-lucide-rotate-ccw" :label="t('rev.restore')" :loading="restoring === r.id" :disabled="!!restoring" @click="restore(r)" />
            </div>
          </div>
          <div v-if="expanded[r.id] !== undefined" class="mt-2 rounded bg-(--ui-bg-muted) p-2 text-xs">
            <p v-if="!expanded[r.id]">{{ t('rev.loading') }}</p>
            <ul v-else class="space-y-0.5">
              <li v-for="a in expanded[r.id]!.agents" :key="a.key" class="flex flex-wrap items-center gap-1.5">
                <span>{{ a.name }}</span>
                <code class="text-(--ui-text-muted)">{{ a.key }}</code>
                <span class="text-(--ui-text-muted)">· {{ a.llm_model || modelTierLabel[a.model_tier] }}</span>
                <UBadge v-if="a.key === expanded[r.id]!.default" :label="t('team.default')" size="sm" variant="subtle" />
              </li>
            </ul>
          </div>
        </li>
      </ol>
    </template>
  </USlideover>
</template>
