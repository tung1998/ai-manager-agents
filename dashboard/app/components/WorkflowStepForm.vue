<script setup lang="ts">
// The canvas's right panel (ADR-108): the selected step's form, the Input
// node's inputs, or (nothing selected) the workflow's name. Fields the office
// would refuse are marked; every change is a new def.
import type { MessageKey } from '~/locales/vi'

const props = defineProps<{
  def: WorkflowDef
  sel: string // '' | '__input' | '__output' | a step's id
  projectId?: string
  bindings?: Record<string, string>
  agents: Agent[]
  workflows: { key: string, name: string, inputs: WorkflowField[] }[]
}>()
const emit = defineEmits<{ 'update:def': [WorkflowDef], 'update:bindings': [Record<string, string>], 'select': [string] }>()
const { t } = useLang()
const NONE = '_'

const steps = computed(() => props.def.steps ?? [])
const step = computed(() => steps.value.find(s => s.id === props.sel))
const missing = computed(() => step.value ? stepMissing(step.value, props.def) : [])
const miss = (f: string) => missing.value.includes(f) ? t('wf.step.missing') : undefined
const ui = { hint: 'text-(--ui-error) text-xs' }

const setDef = (p: Partial<WorkflowDef>) => emit('update:def', { ...props.def, ...p })
function patch(p: Partial<WorkflowStep>) {
  const id = props.sel
  setDef({ steps: steps.value.map(s => s.id === id ? { ...s, ...p } : s) })
}

// the id: renamed on Enter or leaving the field, links and templates follow
const idDraft = ref('')
watch(() => props.sel, () => { idDraft.value = step.value?.id ?? '' }, { immediate: true })
const idBad = computed(() => idDraft.value !== step.value?.id && (!validStepKey(idDraft.value) || steps.value.some(s => s.id === idDraft.value)))
function commitId() {
  const from = step.value?.id
  if (!from || idDraft.value === from) return
  if (idBad.value) { idDraft.value = from; return }
  setDef({ steps: renameStep(steps.value, from, idDraft.value) })
  emit('select', idDraft.value)
}

function remove() {
  const id = props.sel
  const rest = steps.value.filter(s => s.id !== id).map((s) => {
    const n = { ...s }
    for (const f of ['next', 'then', 'else'] as const) if (n[f] === id) n[f] = ''
    return n
  })
  setDef({ steps: rest.length ? rest : [{ id: 'end', type: 'end' }] })
  emit('select', '')
}

// where a link may go
const targetItems = computed(() => [
  { label: t('wf.step.none'), value: NONE },
  ...steps.value.filter(s => s.id !== props.sel).map(s => ({ label: s.name ? `${s.name} · ${s.id}` : s.id, value: s.id, icon: stepIcon(s.type) }))
])
const link = (f: 'next' | 'then' | 'else') => step.value?.[f] || NONE
const setLink = (f: 'next' | 'then' | 'else', v: string) => patch({ [f]: v === NONE ? '' : v })

// the templates usable here: the inputs, and the steps that may run before it
const vars = computed(() => {
  const before = new Set<string>()
  const preds = new Map<string, string[]>()
  for (const s of steps.value) {
    for (const to of [s.next, s.then, s.else]) if (to) preds.set(to, [...(preds.get(to) ?? []), s.id])
  }
  const queue = [props.sel]
  while (queue.length) {
    for (const p of preds.get(queue.shift()!) ?? []) {
      if (!before.has(p)) { before.add(p); queue.push(p) }
    }
  }
  before.delete(props.sel)
  return [
    ...(props.def.inputs ?? []).map(f => `{{input.${f.key}}}`),
    ...steps.value.filter(s => before.has(s.id)).map(s => `{{steps.${s.id}.output}}`)
  ]
})

// ---- agent: its role, the role's access and agent ----
const roleItems = computed(() => props.def.roles.filter(r => !r.workflow).map(r => ({ label: `${r.name} · ${r.key}`, value: r.key })))
const role = computed(() => props.def.roles.find(r => r.key === step.value?.role))
function patchRole(p: Partial<WorkflowRole>) {
  const key = role.value?.key
  setDef({ roles: props.def.roles.map(r => r.key === key ? { ...r, ...p } : r) })
}
function newRole() {
  let k = 'role'
  for (let i = 2; props.def.roles.some(r => r.key === k); i++) k = `role-${i}`
  const id = props.sel
  setDef({ roles: [...props.def.roles, { key: k, name: k, access: 'analyze' }], steps: steps.value.map(s => s.id === id ? { ...s, role: k } : s) })
}
const accessItems = computed(() => (['analyze', 'propose', 'edit'] as const).map(a => ({ label: t(`wf.access.${a}` as MessageKey), value: a, icon: accessIcon[a] })))
const agentItems = computed(() => [{ label: t('wf.step.unbound'), value: NONE }, ...props.agents.map(a => ({ label: a.name, value: a.id }))])
const boundAgent = computed(() => (role.value && props.bindings?.[role.value.key]) || NONE)
function bind(v: string) {
  if (!role.value) return
  const b = { ...(props.bindings ?? {}) }
  if (v === NONE) delete b[role.value.key]
  else b[role.value.key] = v
  emit('update:bindings', b)
}

// ---- workflow: which one, its inputs ----
const wfItems = computed(() => {
  const list = props.workflows.map(w => ({ label: `${w.name} · /${w.key}`, value: w.key }))
  const cur = step.value?.workflow
  if (cur && !list.some(i => i.value === cur)) list.unshift({ label: `#${cur}`, value: cur })
  return list
})
const child = computed(() => props.workflows.find(w => w.key === step.value?.workflow))
function setWorkflow(key: string) {
  const w = props.workflows.find(x => x.key === key)
  const inputs = { ...Object.fromEntries((w?.inputs ?? []).map(f => [f.key, ''])), ...(step.value?.inputs ?? {}) }
  patch({ workflow: key, inputs })
}

const LANGS = ['bash', 'node', 'python']
const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']
const onErrorItems = computed(() => [{ label: t('wf.step.onErrorStop'), value: 'stop' }, { label: t('wf.step.onErrorContinue'), value: 'continue' }])
</script>

<template>
  <!-- the Input node: what a caller gives -->
  <div v-if="sel === '__input'" class="space-y-2 text-sm">
    <p class="flex items-center gap-1.5 font-medium">
      <UIcon name="i-lucide-log-in" class="size-4 text-primary" />{{ t('wf.inputs') }}
      <UTooltip :text="t('wf.canvas.inputsInfo')"><UIcon name="i-lucide-info" class="size-3.5 text-(--ui-text-muted)" /></UTooltip>
    </p>
    <WorkflowFieldsEditor :model-value="def.inputs" @update:model-value="v => setDef({ inputs: v })" />
  </div>

  <!-- the Output node (coordinator mode): what it gives back -->
  <div v-else-if="sel === '__output'" class="space-y-2 text-sm">
    <p class="flex items-center gap-1.5 font-medium"><UIcon name="i-lucide-log-out" class="size-4 text-primary" />{{ t('wf.outputs') }}</p>
    <WorkflowFieldsEditor :model-value="def.outputs" with-type @update:model-value="v => setDef({ outputs: v })" />
  </div>

  <!-- a step -->
  <div v-else-if="step" :key="step.id" class="space-y-3 text-sm">
    <div class="flex items-center gap-1.5">
      <UIcon :name="stepIcon(step.type)" class="size-4 text-primary" />
      <span class="font-medium">{{ t(`wf.step.type.${step.type}` as MessageKey) }}</span>
      <UButton class="ms-auto" size="xs" color="error" variant="ghost" icon="i-lucide-trash-2" :aria-label="t('wf.delete')" @click="remove()" />
    </div>
    <div class="grid grid-cols-2 gap-2">
      <UFormField :label="t('wf.step.id')" :hint="idBad ? t('wf.step.idBad') : miss('id')" :ui="ui">
        <UInput v-model="idDraft" size="sm" class="w-full font-mono" :color="idBad ? 'error' : undefined" @blur="commitId()" @keydown.enter="commitId()" />
      </UFormField>
      <UFormField :label="t('wf.step.name')">
        <UInput :model-value="step.name ?? ''" size="sm" class="w-full" @update:model-value="v => patch({ name: String(v) || undefined })" />
      </UFormField>
    </div>

    <!-- agent -->
    <template v-if="step.type === 'agent'">
      <UFormField :label="t('wf.step.role')" :hint="miss('role')" :ui="ui">
        <div class="flex gap-1">
          <USelect :model-value="step.role || undefined" :items="roleItems" size="sm" class="min-w-0 flex-1" :placeholder="t('wf.step.pickRole')" @update:model-value="v => patch({ role: String(v) })" />
          <UTooltip :text="t('wf.step.newRole')"><UButton size="sm" color="neutral" variant="outline" icon="i-lucide-plus" @click="newRole()" /></UTooltip>
        </div>
      </UFormField>
      <div v-if="role" class="space-y-2 rounded-md border border-(--ui-border) p-2">
        <div class="grid grid-cols-2 gap-2">
          <UInput :model-value="role.name" size="xs" :placeholder="t('wf.step.roleName')" @update:model-value="v => patchRole({ name: String(v) })" />
          <USelect :model-value="role.access" :items="accessItems" size="xs" @update:model-value="v => patchRole({ access: v as WorkflowAccess })" />
        </div>
        <UFormField v-if="projectId" :label="t('wf.step.agent')" size="xs">
          <USelect :model-value="boundAgent" :items="agentItems" size="xs" class="w-full" @update:model-value="v => bind(String(v))" />
        </UFormField>
      </div>
      <UFormField :label="t('wf.step.prompt')" :hint="miss('prompt')" :ui="ui">
        <UTextarea :model-value="step.prompt ?? ''" :rows="5" autoresize :maxrows="16" class="w-full" @update:model-value="v => patch({ prompt: String(v) })" />
      </UFormField>
    </template>

    <!-- a sub-workflow -->
    <template v-else-if="step.type === 'workflow'">
      <UFormField :label="t('wf.step.workflow')" :hint="miss('workflow')" :ui="ui">
        <USelect :model-value="step.workflow || undefined" :items="wfItems" size="sm" class="w-full" :placeholder="t('wf.step.pickWorkflow')" @update:model-value="v => setWorkflow(String(v))" />
      </UFormField>
      <UFormField :label="t('wf.inputs')">
        <WorkflowKvEditor :model-value="step.inputs" :fixed="child?.inputs.map(f => f.key)" :required="child?.inputs.filter(f => f.required).map(f => f.key)" @update:model-value="v => patch({ inputs: v })" />
      </UFormField>
    </template>

    <!-- code -->
    <template v-else-if="step.type === 'code'">
      <div class="grid grid-cols-2 gap-2">
        <UFormField :label="t('wf.step.lang')" :hint="miss('lang')" :ui="ui">
          <USelect :model-value="step.lang || undefined" :items="LANGS" size="sm" class="w-full" @update:model-value="v => patch({ lang: v as WorkflowStep['lang'] })" />
        </UFormField>
        <UFormField :label="t('wf.step.timeout')">
          <UInputNumber :model-value="step.timeout_s ?? 0" :min="0" size="sm" class="w-full" @update:model-value="v => patch({ timeout_s: v || undefined })" />
        </UFormField>
      </div>
      <UFormField :label="t('wf.step.script')" :hint="miss('script')" :ui="ui">
        <UTextarea :model-value="step.script ?? ''" :rows="8" autoresize :maxrows="24" class="w-full font-mono text-xs" @update:model-value="v => patch({ script: String(v) })" />
      </UFormField>
      <p class="text-xs text-(--ui-text-muted)">{{ t('wf.step.codeInfo') }}</p>
    </template>

    <!-- http -->
    <template v-else-if="step.type === 'http'">
      <div class="flex gap-2">
        <USelect :model-value="(step.method || 'GET').toUpperCase()" :items="METHODS" size="sm" class="w-24" @update:model-value="v => patch({ method: String(v) })" />
        <UFormField class="min-w-0 flex-1" :hint="miss('url')" :ui="ui">
          <UInput :model-value="step.url ?? ''" size="sm" class="w-full font-mono" placeholder="https://…" @update:model-value="v => patch({ url: String(v) })" />
        </UFormField>
      </div>
      <UFormField :label="t('wf.step.headers')">
        <WorkflowKvEditor :model-value="step.headers" key-placeholder="Header" @update:model-value="v => patch({ headers: Object.keys(v).length ? v : undefined })" />
      </UFormField>
      <UFormField :label="t('wf.step.body')">
        <UTextarea :model-value="step.body ?? ''" :rows="4" autoresize :maxrows="16" class="w-full font-mono text-xs" @update:model-value="v => patch({ body: String(v) || undefined })" />
      </UFormField>
      <UFormField :label="t('wf.step.timeout')">
        <UInputNumber :model-value="step.timeout_s ?? 0" :min="0" size="sm" class="w-32" @update:model-value="v => patch({ timeout_s: v || undefined })" />
      </UFormField>
    </template>

    <!-- condition -->
    <template v-else-if="step.type === 'condition'">
      <UFormField :label="t('wf.step.if')" :hint="miss('if')" :ui="ui">
        <UInput :model-value="step.if ?? ''" size="sm" class="w-full font-mono" placeholder="{{steps.a.status}} == 200" @update:model-value="v => patch({ if: String(v) })" />
        <template #help><span class="text-xs">{{ t('wf.step.ifInfo') }}</span></template>
      </UFormField>
      <div class="grid grid-cols-2 gap-2">
        <UFormField :label="t('wf.canvas.yes')" :hint="miss('then')" :ui="ui">
          <USelect :model-value="link('then')" :items="targetItems" size="sm" class="w-full" @update:model-value="v => setLink('then', String(v))" />
        </UFormField>
        <UFormField :label="t('wf.canvas.no')" :hint="miss('else')" :ui="ui">
          <USelect :model-value="link('else')" :items="targetItems" size="sm" class="w-full" @update:model-value="v => setLink('else', String(v))" />
        </UFormField>
      </div>
      <UFormField :label="t('wf.step.maxLoops')">
        <template #hint><UTooltip :text="t('wf.step.maxLoopsInfo')"><UIcon name="i-lucide-info" class="size-3.5 text-(--ui-text-muted)" /></UTooltip></template>
        <UInputNumber :model-value="step.max_loops ?? 0" :min="0" :max="50" size="sm" class="w-32" @update:model-value="v => patch({ max_loops: v || undefined })" />
      </UFormField>
    </template>

    <!-- approve / check -->
    <template v-else-if="step.type === 'approve' || step.type === 'check'">
      <UFormField v-if="step.type === 'approve'" :label="t('wf.step.note')" :hint="miss('note')" :ui="ui">
        <UTextarea :model-value="step.note ?? ''" :rows="3" autoresize class="w-full" @update:model-value="v => patch({ note: String(v) })" />
      </UFormField>
      <UFormField v-else :label="t('wf.step.command')" :hint="miss('command')" :ui="ui">
        <UInput :model-value="step.command ?? ''" size="sm" class="w-full font-mono" placeholder="pnpm test" @update:model-value="v => patch({ command: String(v) })" />
      </UFormField>
    </template>

    <!-- end -->
    <template v-else-if="step.type === 'end'">
      <UFormField :label="t('wf.step.summary')">
        <UTextarea :model-value="step.summary ?? ''" :rows="3" autoresize class="w-full" @update:model-value="v => patch({ summary: String(v) || undefined })" />
      </UFormField>
      <UFormField v-if="def.outputs?.length" :label="t('wf.step.outputValues')">
        <WorkflowKvEditor
          :model-value="step.outputs" locked :fixed="def.outputs.map(f => f.key)" :required="def.outputs.filter(f => f.required).map(f => f.key)"
          @update:model-value="v => patch({ outputs: Object.fromEntries(Object.entries(v).filter(([k]) => def.outputs?.some(f => f.key === k))) })"
        />
      </UFormField>
      <UFormField :label="t('wf.outputs')">
        <template #hint><UTooltip :text="t('wf.canvas.outputsInfo')"><UIcon name="i-lucide-info" class="size-3.5 text-(--ui-text-muted)" /></UTooltip></template>
        <WorkflowFieldsEditor :model-value="def.outputs" with-type @update:model-value="v => setDef({ outputs: v })" />
      </UFormField>
    </template>

    <!-- where it goes next -->
    <div v-if="step.type !== 'end' && step.type !== 'condition'" class="grid grid-cols-2 gap-2">
      <UFormField :label="t('wf.step.next')" :hint="miss('next')" :ui="ui">
        <USelect :model-value="link('next')" :items="targetItems" size="sm" class="w-full" @update:model-value="v => setLink('next', String(v))" />
      </UFormField>
      <UFormField v-if="step.type === 'approve' || step.type === 'check'" :label="t('wf.canvas.notPassed')" :hint="miss('else')" :ui="ui">
        <USelect :model-value="link('else')" :items="targetItems" size="sm" class="w-full" @update:model-value="v => setLink('else', String(v))" />
      </UFormField>
    </div>
    <UFormField v-if="step.type !== 'end' && step.type !== 'condition'" :label="t('wf.step.onError')">
      <USelect :model-value="step.on_error || 'stop'" :items="onErrorItems" size="sm" class="w-full" @update:model-value="v => patch({ on_error: v === 'stop' ? undefined : v as WorkflowStep['on_error'] })" />
    </UFormField>

    <div class="space-y-1 border-t border-(--ui-border) pt-2">
      <p class="flex items-center gap-1 text-xs text-(--ui-text-muted)">
        {{ t('wf.step.vars') }}
        <UTooltip :text="t('wf.step.varsInfo')"><UIcon name="i-lucide-info" class="size-3.5" /></UTooltip>
      </p>
      <div class="flex flex-wrap gap-1">
        <code v-for="v in vars" :key="v" class="rounded bg-(--ui-bg-elevated) px-1 font-mono text-[11px]">{{ v }}</code>
        <span v-if="!vars.length" class="text-xs text-(--ui-text-dimmed)">{{ t('wf.step.noVars') }}</span>
      </div>
    </div>
  </div>

  <!-- nothing selected: the workflow itself -->
  <div v-else class="space-y-3 text-sm">
    <p class="text-xs text-(--ui-text-muted)">{{ t(steps.length ? 'wf.canvas.pickNode' : 'wf.canvas.coordHint') }}</p>
    <UFormField :label="t('wf.canvas.key')" :hint="validStepKey(def.key) ? undefined : t('wf.step.missing')" :ui="ui">
      <UInput :model-value="def.key" size="sm" class="w-full font-mono" @update:model-value="v => setDef({ key: String(v).trim() })" />
    </UFormField>
    <UFormField :label="t('wf.canvas.name')" :hint="def.name.trim() ? undefined : t('wf.step.missing')" :ui="ui">
      <UInput :model-value="def.name" size="sm" class="w-full" @update:model-value="v => setDef({ name: String(v) })" />
    </UFormField>
    <UFormField :label="t('wf.canvas.description')">
      <UTextarea :model-value="def.description" :rows="2" autoresize class="w-full" @update:model-value="v => setDef({ description: String(v) })" />
    </UFormField>
    <UFormField v-if="!steps.length" :label="t('wf.inputs')">
      <WorkflowFieldsEditor :model-value="def.inputs" @update:model-value="v => setDef({ inputs: v })" />
    </UFormField>
    <UFormField v-if="!steps.length" :label="t('wf.outputs')">
      <WorkflowFieldsEditor :model-value="def.outputs" with-type @update:model-value="v => setDef({ outputs: v })" />
    </UFormField>
  </div>
</template>
