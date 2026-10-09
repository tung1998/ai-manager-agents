<script setup lang="ts">
import type { Permissions as AgentPermissions } from '~/composables/useOffice'

interface AgentDoc { path: string, kind: string, name?: string, description?: string }
interface Summary {
  name: string
  description: string
  manifests: string[]
  frameworks: string[]
  services: string[]
  infra: string[]
  languages: Record<string, number>
  agent_docs: AgentDoc[]
  readme: string
  file_count: number
  env_vars: string[]
}
interface AgentChange {
  action: 'update' | 'add' | 'remove'
  key: string
  name?: string
  role?: string
  model_tier?: ModelTier
  instructions?: string
  level?: PermLevel
  caps?: string[]
  commands?: string[]
  source?: string
  reason: string
}
interface PackSpec {
  key: string
  name: string
  default?: string
  agents: { key: string, name: string, role?: string, model_tier?: ModelTier, permissions: AgentPermissions }[]
}
interface Proposal {
  description: string
  pack_key: string
  reason: string
  confidence: number
  agent_changes: AgentChange[]
  skills: Pick[]
  mcp: Pick[]
  quick_checks: string
  notes: string[]
}
interface Pick { name: string, reason: string, needs?: 'key' | 'login' }
interface ReadyItem { kind: string, name: string, status: 'ok' | 'todo' | 'error', detail?: string }
interface ProposeResult {
  proposal: Proposal
  pack: PackSpec
  problems: string[]
  warnings: string[]
  provider: string
  model: string
  usage: { input_tokens: number, output_tokens: number, cost_usd?: number, duration_ms: number }
  suggested_workflows: string[]
}

const route = useRoute()
const toast = useToast()
const { t } = useLang()
const id = computed(() => route.params.id as string)
const here = computed(() => `/projects/${id.value}/setup`)

const _f1 = useLiveFetch<{ project: Project }>(() => `/api/projects/${id.value}`)
const { data: projData } = _f1
const _f2 = useLiveFetch<{ providers: Provider[] }>('/api/providers')
const { data: provData } = _f2
await Promise.all([_f1, _f2]) // started together: one round trip, not 2 (a phone over a VPN)
const project = computed(() => projData.value?.project)
const readyProvider = computed(() => provData.value?.providers.find(p => p.is_default && p.status === 'ok')
  ?? provData.value?.providers.find(p => p.status === 'ok'))

function goConnect() {
  toast.add({ title: t('setup.needProvider'), description: t('setup.needProviderDesc'), color: 'warning' })
  return navigateTo({ path: '/providers', query: { next: here.value } })
}

// Redirect straight away when no AI connection works.
if (!readyProvider.value) {
  await goConnect()
}

// ---- step 1: scan ----
const summary = ref<Summary | null>(null)
const scanning = ref(false)
const goal = ref('')
async function runScan() {
  if (project.value?.scope !== 'folder') return
  scanning.value = true
  try {
    const res = await $fetch<{ summary: Summary }>(`/api/projects/${id.value}/setup/scan`, { method: 'POST', body: {} })
    summary.value = res.summary
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    scanning.value = false
  }
}
onMounted(runScan)

const topLanguages = computed(() => Object.entries(summary.value?.languages ?? {}).sort((a, b) => b[1] - a[1]).slice(0, 6))
const docKind = computed<Record<string, string>>(() => ({ instructions: t('setup.docInstructions'), subagent: t('setup.docSubagent'), skill: t('setup.docSkill'), rules: t('setup.docRules') }))

// ---- step 2: propose ----
const proposing = ref(false)
const result = ref<ProposeResult | null>(null)
const description = ref('')
const packKey = ref('')
const accepted = ref<boolean[]>([])
const workflowsAccepted = ref<boolean[]>([])
const workflowLabel = computed<Record<string, string>>(() => ({ 'fix-tests': t('setup.wfFixTests'), 'review-pr': t('setup.wfReviewPr') }))
const preview = ref<{ pack: PackSpec, problems: string[], warnings: string[] } | null>(null)
const skillsAccepted = ref<boolean[]>([])
const mcpAccepted = ref<boolean[]>([])
const quickChecks = ref('')
const useQuickChecks = ref(true)
const ready = ref<ReadyItem[] | null>(null)

async function propose() {
  proposing.value = true
  try {
    const res = await $fetch<{ result: ProposeResult }>(`/api/projects/${id.value}/setup/propose`, { method: 'POST', body: { goal: goal.value } })
    result.value = res.result
    description.value = res.result.proposal.description
    packKey.value = res.result.proposal.pack_key
    accepted.value = res.result.proposal.agent_changes.map(() => true)
    workflowsAccepted.value = res.result.suggested_workflows.map(() => true)
    skillsAccepted.value = res.result.proposal.skills.map(() => true)
    mcpAccepted.value = res.result.proposal.mcp.map(() => true)
    quickChecks.value = res.result.proposal.quick_checks
    useQuickChecks.value = true
    ready.value = null
    preview.value = { pack: res.result.pack, problems: res.result.problems, warnings: res.result.warnings }
  } catch (e) {
    const d = (e as { data?: { code?: string, error?: string } }).data
    if (d?.code === 'no_provider') return goConnect()
    if (d?.code === 'budget') {
      toast.add({ title: t('setup.needProviderBudget'), description: d.error, color: 'warning', actions: [{ label: t('setup.viewCosts'), onClick: () => { navigateTo(`/projects/${route.params.id}?tab=info`) } }] })
      return
    }
    toast.add({ title: t('setup.proposeFailed'), description: apiError(e), color: 'error' })
  } finally {
    proposing.value = false
  }
}

const chosenChanges = computed(() => (result.value?.proposal.agent_changes ?? []).filter((_, i) => accepted.value[i]))
const chosenWorkflows = computed(() => (result.value?.suggested_workflows ?? []).filter((_, i) => workflowsAccepted.value[i]))
const chosenSkills = computed(() => (result.value?.proposal.skills ?? []).filter((_, i) => skillsAccepted.value[i]).map(p => p.name))
const chosenMCP = computed(() => (result.value?.proposal.mcp ?? []).filter((_, i) => mcpAccepted.value[i]).map(p => p.name))

// Rebuild the preview whenever the pack or ticked changes move.
watch([packKey, accepted, workflowsAccepted], async () => {
  if (!result.value || !packKey.value) return
  try {
    preview.value = await $fetch(`/api/projects/${id.value}/setup/build`, {
      method: 'POST', body: { pack_key: packKey.value, changes: chosenChanges.value, workflows: chosenWorkflows.value }
    })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}, { deep: true })

const applying = ref(false)
async function apply() {
  applying.value = true
  try {
    const res = await $fetch<{ ready: ReadyItem[] }>(`/api/projects/${id.value}/setup/apply`, {
      method: 'POST',
      body: {
        pack_key: packKey.value, changes: chosenChanges.value, workflows: chosenWorkflows.value, description: description.value,
        skills: chosenSkills.value, mcp: chosenMCP.value, quick_checks: useQuickChecks.value ? quickChecks.value : undefined
      }
    })
    toast.add({ title: t('setup.applyDone'), color: 'success' })
    ready.value = res.ready
  } catch (e) {
    const d = (e as { data?: { problems?: string[] } }).data
    toast.add({ title: t('setup.applyFailed'), description: d?.problems?.join('; ') ?? apiError(e), color: 'error' })
  } finally {
    applying.value = false
  }
}

// The resolved permission level/caps after an agent change is applied: looked
// up from the preview's pack (built server-side via internal/perm.Agent), not
// the AI's raw proposal — an invalid level there is dropped and reported as a
// warning, and caps can raise the real level above what was asked for.
function changeAgent(key: string) { return preview.value?.pack.agents.find(a => a.key === key) }
function changeLevel(key: string): PermLevel | null {
  const a = changeAgent(key)
  return a ? agentLevel(a.permissions) : null
}
function changeCaps(key: string): string[] {
  const a = changeAgent(key)
  return a ? agentCaps(a.permissions) : []
}

const readyMeta: Record<ReadyItem['status'], { icon: string, color: string }> = {
  ok: { icon: 'i-lucide-circle-check', color: 'text-(--ui-success)' },
  todo: { icon: 'i-lucide-circle-alert', color: 'text-(--ui-warning)' },
  error: { icon: 'i-lucide-circle-x', color: 'text-(--ui-error)' }
}
const readyKind = computed<Record<string, string>>(() => ({
  agents: t('setup.readyAgents'), workflows: t('setup.readyWorkflows'), skill: 'Skill', mcp: 'MCP',
  quick_check: t('setup.readyQuickCheck'), command: t('setup.readyCommands')
}))
const needsLabel = computed<Record<string, string>>(() => ({ key: t('setup.needsKey'), login: t('setup.needsLogin') }))

const actionMeta = computed<Record<string, { label: string, color: 'info' | 'success' | 'error' }>>(() => ({
  update: { label: t('setup.actionUpdate'), color: 'info' },
  add: { label: t('setup.actionAdd'), color: 'success' },
  remove: { label: t('setup.actionRemove'), color: 'error' }
}))
</script>

<template>
  <PageShell :title="t('setup.title', { name: project?.name ?? '' })">
    <template #actions>
      <UButton :to="`/projects/${id}`" icon="i-lucide-arrow-left" :label="t('setup.back')" color="neutral" variant="ghost" />
    </template>

    <div v-if="project" class="mx-auto max-w-5xl space-y-6">
      <UAlert
        v-if="project.agent_count" color="warning" variant="subtle" icon="i-lucide-triangle-alert"
        :description="t('team.setup.hasAgentsWarn', { n: project.agent_count })"
      />

      <!-- step 1 -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2">
              <span class="flex size-6 items-center justify-center rounded-full bg-(--ui-primary) text-xs font-semibold text-white">1</span>
              <p class="font-medium">{{ t('setup.step1') }}</p>
            </div>
            <UButton v-if="project.scope === 'folder'" icon="i-lucide-refresh-cw" size="xs" color="neutral" variant="ghost" :loading="scanning" :label="t('setup.rescan')" @click="runScan" />
          </div>
        </template>

        <div v-if="project.scope === 'machine'" class="text-sm text-(--ui-text-muted)">
          {{ t('setup.machineNoScan') }}
        </div>
        <div v-else-if="scanning && !summary" class="text-sm text-(--ui-text-muted)">{{ t('setup.scanning') }}</div>
        <div v-else-if="summary" class="grid gap-4 md:grid-cols-2">
          <div class="space-y-3 text-sm">
            <div>
              <p class="text-xs text-(--ui-text-muted)">{{ t('setup.stack') }}</p>
              <div class="mt-1 flex flex-wrap gap-1">
                <UBadge v-for="f in summary.frameworks" :key="f" :label="f" variant="subtle" />
                <UBadge v-for="[l, n] in topLanguages" :key="l" :label="`${l} ${n}`" color="neutral" variant="outline" />
              </div>
            </div>
            <div v-if="summary.services.length">
              <p class="text-xs text-(--ui-text-muted)">{{ t('setup.services') }}</p>
              <div class="mt-1 flex flex-wrap gap-1">
                <UBadge v-for="s in summary.services" :key="s" :label="s" color="info" variant="subtle" />
              </div>
            </div>
            <div v-if="summary.infra.length">
              <p class="text-xs text-(--ui-text-muted)">{{ t('setup.infra') }}</p>
              <p>{{ summary.infra.join(' · ') }}</p>
            </div>
            <p class="text-xs text-(--ui-text-muted)">{{ t('setup.fileCount', { n: summary.file_count, manifests: summary.manifests.join(', ') || t('setup.noManifest') }) }}</p>
          </div>
          <div class="text-sm">
            <p class="text-xs text-(--ui-text-muted)">{{ t('setup.agentDocs', { n: summary.agent_docs.length }) }}</p>
            <ul v-if="summary.agent_docs.length" class="mt-1 space-y-1">
              <li v-for="d in summary.agent_docs" :key="d.path" class="flex items-center gap-2">
                <UBadge :label="docKind[d.kind] ?? d.kind" size="sm" color="neutral" variant="soft" />
                <code class="truncate text-xs">{{ d.path }}</code>
              </li>
            </ul>
            <p v-else class="mt-1 text-(--ui-text-muted)">{{ t('setup.noAgentDocs') }}</p>
          </div>
        </div>
      </UCard>

      <!-- step 2 -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2">
            <span class="flex size-6 items-center justify-center rounded-full bg-(--ui-primary) text-xs font-semibold text-white">2</span>
            <p class="font-medium">{{ t('setup.step2') }}</p>
          </div>
        </template>
        <div class="space-y-3">
          <UFormField :label="project.scope === 'machine' ? t('setup.goalLabelMachine') : t('setup.goalLabelFolder')" :required="project.scope === 'machine'">
            <UTextarea
              v-model="goal" :rows="3" class="w-full"
              :placeholder="t('setup.goalPlaceholder')"
            />
          </UFormField>
          <div class="flex flex-wrap items-center gap-3">
            <UButton
              icon="i-lucide-sparkles" :label="result ? t('setup.analyzeAgain') : t('setup.analyze')" :loading="proposing"
              :disabled="project.scope === 'machine' && !goal.trim()" @click="propose"
            />
            <span v-if="readyProvider" class="text-xs text-(--ui-text-muted)">
              {{ t('setup.usingProvider', { name: readyProvider.name, model: readyProvider.tier_models.strong || '' }) }}
            </span>
          </div>
          <p v-if="proposing" class="text-sm text-(--ui-text-muted)">{{ t('setup.analyzing') }}</p>
        </div>
      </UCard>

      <!-- step 3 -->
      <UCard v-if="result">
        <template #header>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-2">
              <span class="flex size-6 items-center justify-center rounded-full bg-(--ui-primary) text-xs font-semibold text-white">3</span>
              <p class="font-medium">{{ t('setup.step3') }}</p>
            </div>
            <span class="text-xs text-(--ui-text-muted)">
              {{ result.provider }} · {{ result.model }} · {{ result.usage.input_tokens }} in / {{ result.usage.output_tokens }} out
              · {{ Math.round(result.usage.duration_ms / 1000) }}s
              <template v-if="result.usage.cost_usd"> · ${{ result.usage.cost_usd.toFixed(3) }}</template>
            </span>
          </div>
        </template>

        <div class="space-y-6">
          <UFormField :label="t('setup.projectDesc')" :help="t('setup.projectDescHelp')">
            <UTextarea v-model="description" :rows="3" class="w-full" />
          </UFormField>

          <div class="grid gap-6 lg:grid-cols-2">
            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <p class="text-sm font-medium">{{ t('team.setup.pack') }}</p>
                <UBadge :label="t('setup.confidence', { n: Math.round(result.proposal.confidence * 100) })" color="neutral" variant="soft" size="sm" />
              </div>
              <p class="text-sm text-(--ui-text-muted)">{{ result.proposal.reason }}</p>
              <PackPicker v-model="packKey" />
            </div>

            <div class="space-y-2">
              <p class="text-sm font-medium">{{ t('setup.preview') }}</p>
              <ul class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
                <li v-for="a in preview?.pack.agents ?? []" :key="a.key" class="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
                  <span class="font-medium">{{ a.name }}</span>
                  <UBadge v-if="a.key === preview?.pack.default" :label="t('team.default')" icon="i-lucide-star" variant="subtle" size="sm" />
                  <UBadge v-if="a.model_tier" :label="modelTierLabel[a.model_tier]" color="neutral" variant="outline" size="sm" />
                  <UBadge :label="permOf(agentLevel(a.permissions)).label" :icon="permOf(agentLevel(a.permissions)).icon" color="neutral" variant="soft" size="sm" />
                  <span v-if="a.role" class="w-full truncate text-xs text-(--ui-text-muted)">{{ a.role }}</span>
                </li>
              </ul>
              <UAlert v-if="preview?.problems.length" color="error" variant="subtle" :title="t('setup.invalidTitle')">
                <template #description>
                  <ul class="list-disc ps-4">
                    <li v-for="p in preview.problems" :key="p">{{ p }}</li>
                  </ul>
                </template>
              </UAlert>
              <UAlert v-if="preview?.warnings.length" color="warning" variant="subtle" :title="t('setup.warningsTitle')">
                <template #description>
                  <ul class="list-disc ps-4">
                    <li v-for="w in preview.warnings" :key="w">{{ w }}</li>
                  </ul>
                </template>
              </UAlert>
            </div>
          </div>

          <div v-if="result.proposal.agent_changes.length" class="space-y-2">
            <p class="text-sm font-medium">{{ t('setup.refineAgents', { n: chosenChanges.length, total: result.proposal.agent_changes.length }) }}</p>
            <div
              v-for="(c, i) in result.proposal.agent_changes" :key="i"
              class="flex gap-3 rounded-lg border p-3"
              :class="accepted[i] ? 'border-(--ui-border)' : 'border-dashed border-(--ui-border) opacity-60'"
            >
              <UCheckbox v-model="accepted[i]" class="mt-0.5" />
              <div class="min-w-0 flex-1 space-y-1">
                <div class="flex flex-wrap items-center gap-2">
                  <UBadge :label="actionMeta[c.action]?.label ?? c.action" :color="actionMeta[c.action]?.color ?? 'neutral'" variant="subtle" size="sm" />
                  <span class="font-medium">{{ c.name || c.key }}</span>
                  <code class="text-xs text-(--ui-text-muted)">{{ c.key }}</code>
                  <UBadge v-if="c.model_tier" :label="modelTierLabel[c.model_tier]" size="sm" color="neutral" variant="outline" />
                  <UBadge v-if="changeLevel(c.key)" :label="permOf(changeLevel(c.key)!).label" :icon="permOf(changeLevel(c.key)!).icon" size="sm" color="neutral" variant="soft" />
                  <UBadge v-for="cap in changeCaps(c.key)" :key="cap" :label="permCaps.find(p => p.id === cap)?.label ?? cap" size="sm" color="neutral" variant="outline" />
                </div>
                <p class="text-sm text-(--ui-text-muted)">{{ c.reason }}</p>
                <p v-if="c.source" class="text-xs">{{ t('setup.fromFilePrefix') }} <code>{{ c.source }}</code></p>
                <p v-if="c.commands?.length" class="flex flex-wrap items-center gap-1 text-xs text-(--ui-text-muted)">
                  {{ t('setup.commandsPrefix') }}
                  <code v-for="cmd in c.commands" :key="cmd" class="rounded bg-(--ui-bg-muted) px-1">{{ cmd }}</code>
                </p>
                <details v-if="c.instructions" class="text-sm">
                  <summary class="cursor-pointer text-xs text-(--ui-text-muted)">
                    {{ c.action === 'update' ? t('setup.instructionsAdded') : t('setup.instructions') }}
                  </summary>
                  <p class="mt-1 whitespace-pre-wrap rounded bg-(--ui-bg-muted) p-2 text-xs">{{ c.instructions }}</p>
                </details>
              </div>
            </div>
          </div>

          <div v-if="result.suggested_workflows.length" class="space-y-2">
            <p class="text-sm font-medium">{{ t('setup.suggestedWorkflows', { n: chosenWorkflows.length, total: result.suggested_workflows.length }) }}</p>
            <div v-for="(w, i) in result.suggested_workflows" :key="w" class="flex items-center gap-2 rounded-lg border p-3" :class="workflowsAccepted[i] ? 'border-(--ui-border)' : 'border-dashed border-(--ui-border) opacity-60'">
              <UCheckbox v-model="workflowsAccepted[i]" />
              <span class="font-medium">{{ workflowLabel[w] ?? w }}</span>
            </div>
          </div>

          <div v-if="result.proposal.skills.length || result.proposal.mcp.length" class="space-y-2">
            <p class="text-sm font-medium">{{ t('setup.tools', { n: chosenSkills.length + chosenMCP.length, total: result.proposal.skills.length + result.proposal.mcp.length }) }}</p>
            <div
              v-for="(p, i) in result.proposal.skills" :key="`s-${p.name}`" class="flex gap-3 rounded-lg border p-3"
              :class="skillsAccepted[i] ? 'border-(--ui-border)' : 'border-dashed border-(--ui-border) opacity-60'"
            >
              <UCheckbox v-model="skillsAccepted[i]" class="mt-0.5" />
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <UBadge label="Skill" color="neutral" variant="outline" size="sm" />
                  <span class="font-medium">{{ p.name }}</span>
                </div>
                <p class="text-sm text-(--ui-text-muted)">{{ p.reason }}</p>
              </div>
            </div>
            <div
              v-for="(p, i) in result.proposal.mcp" :key="`m-${p.name}`" class="flex gap-3 rounded-lg border p-3"
              :class="mcpAccepted[i] ? 'border-(--ui-border)' : 'border-dashed border-(--ui-border) opacity-60'"
            >
              <UCheckbox v-model="mcpAccepted[i]" class="mt-0.5" />
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <UBadge label="MCP" color="neutral" variant="outline" size="sm" />
                  <span class="font-medium">{{ p.name }}</span>
                  <UBadge v-if="p.needs" :label="needsLabel[p.needs]" color="warning" variant="subtle" size="sm" />
                </div>
                <p class="text-sm text-(--ui-text-muted)">{{ p.reason }}</p>
              </div>
            </div>
          </div>

          <div class="space-y-2">
            <div class="flex items-center gap-2">
              <UCheckbox v-model="useQuickChecks" />
              <p class="text-sm font-medium">{{ t('setup.quickChecks') }}</p>
            </div>
            <UTextarea
              v-if="useQuickChecks" v-model="quickChecks" :rows="2" autoresize class="w-full font-mono text-xs"
              :placeholder="t('setup.quickChecksPlaceholder')"
            />
          </div>

          <UAlert v-if="result.proposal.notes.length" color="info" variant="subtle" icon="i-lucide-lightbulb" :title="t('setup.moreSuggestions')">
            <template #description>
              <ul class="list-disc ps-4">
                <li v-for="n in result.proposal.notes" :key="n">{{ n }}</li>
              </ul>
            </template>
          </UAlert>

          <div v-if="ready" class="space-y-2 rounded-lg border border-(--ui-border) p-3">
            <p class="text-sm font-medium">{{ t('setup.readyTitle') }}</p>
            <ul class="space-y-1 text-sm">
              <li v-for="(r, i) in ready" :key="i" class="flex items-start gap-2">
                <UIcon :name="readyMeta[r.status].icon" class="mt-0.5 size-4 shrink-0" :class="readyMeta[r.status].color" />
                <span class="font-medium">{{ readyKind[r.kind] ?? r.kind }}<template v-if="r.kind === 'skill' || r.kind === 'mcp'"> {{ r.name }}</template></span>
                <span v-if="r.detail" class="min-w-0 break-words text-(--ui-text-muted)">{{ r.detail }}</span>
              </li>
            </ul>
            <div class="flex justify-end">
              <UButton :to="`/projects/${id}`" icon="i-lucide-arrow-right" :label="t('setup.openProject')" />
            </div>
          </div>

          <div v-else class="flex justify-end gap-2">
            <UButton :to="`/projects/${id}`" color="neutral" variant="ghost" :label="t('common.cancel')" />
            <UButton icon="i-lucide-check" :label="t('setup.applySetup')" :loading="applying" :disabled="!!preview?.problems.length" @click="apply" />
          </div>
        </div>
      </UCard>
    </div>
  </PageShell>
</template>
