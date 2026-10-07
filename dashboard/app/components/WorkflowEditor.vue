<script setup lang="ts">
// Writing a workflow (ADR-098): its file on one side, checked as it is
// written, its own chat on the other; the chat's ```workflow blocks replace
// the file and nothing is saved until Save.
// chatProjectId: where the chat lives (the office assistant's project for the
// library). libKey: a library workflow to edit; projectId (+ workflowId): a
// project's own copy. Neither key nor id: a new one.
const props = defineProps<{ chatProjectId: string, projectId?: string, workflowId?: string, libKey?: string }>()
const toast = useToast()
const saveError = useSaveError()
const { t } = useLang()
const { isAdmin } = useAuth()
const library = computed(() => !props.projectId)
const editing = computed(() => !!(library.value ? props.libKey : props.workflowId))
const back = computed(() => library.value ? { path: '/workflows' } : { path: `/projects/${props.projectId}`, query: { tab: 'workflows' } })

const source = ref('')
const loaded = ref(!editing.value)
onMounted(async () => {
  if (!editing.value) return
  try {
    source.value = library.value
      ? (await $fetch<{ workflow: LibraryWorkflow }>(`/api/workflow-library/${props.libKey}`)).workflow.source ?? ''
      : (await $fetch<{ workflow: ProjectWorkflow }>(`/api/workflows/${props.workflowId}`)).workflow.source
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    loaded.value = true
  }
})

// checked as it is written: the preview, or what is wrong
const def = ref<WorkflowDef | null>(null)
const error = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
watch(source, (s) => {
  clearTimeout(timer)
  timer = setTimeout(async () => {
    if (!s.trim()) { def.value = null; error.value = ''; return }
    try {
      const res = await $fetch<{ ok: boolean, def?: WorkflowDef, error?: string }>('/api/workflow-library/validate', { method: 'POST', body: { source: s } })
      def.value = res.ok ? res.def ?? null : null
      error.value = res.ok ? '' : res.error ?? ''
    } catch { /* checked again on Save */ }
  }, 400)
})

// the chat next to it fills the editor
const highlight = ref(false)
function applyPatch(p: Record<string, unknown>) {
  if (typeof p.source !== 'string') return
  source.value = p.source
  highlight.value = true
  setTimeout(() => { highlight.value = false }, 3000)
  toast.add({ title: t('wf.filled'), color: 'info' })
}
// back on its chat (?c=): a new workflow takes up the chat's last draft; a
// saved one keeps what it says
function replay(blocks: Record<string, unknown>[]) {
  const last = [...blocks].reverse().find(b => typeof b.source === 'string')
  if (editing.value || !last) return
  source.value = last.source as string
}
const pageContext = () => JSON.stringify({ page: editing.value ? 'workflow.edit' : 'workflow.new', scope: library.value ? 'library' : 'project', draft: source.value, error: error.value })

const saving = ref(false)
async function save() {
  saving.value = true
  try {
    if (library.value) {
      const res = await $fetch<{ ok: boolean, def?: WorkflowDef, error?: string }>('/api/workflow-library/validate', { method: 'POST', body: { source: source.value } })
      if (!res.ok || !res.def) {
        error.value = res.error ?? ''
        return toast.add({ title: t('wf.invalid'), description: res.error, color: 'error' })
      }
      const key = res.def.key
      if (editing.value && key !== props.libKey) return toast.add({ title: t('wf.keyChanged', { key: props.libKey ?? '' }), color: 'error' })
      if (!editing.value) { // a new one over one of the library's: asked first
        const taken = await $fetch(`/api/workflow-library/${key}`).then(() => true, () => false)
        if (taken && !confirm(t('wf.overwrite', { key }))) return
      }
      await $fetch(`/api/workflow-library/${key}`, { method: 'PUT', body: { source: source.value } })
    } else if (editing.value) {
      await $fetch(`/api/workflows/${props.workflowId}`, { method: 'PATCH', body: { source: source.value } })
    } else {
      await $fetch(`/api/projects/${props.projectId}/workflows`, { method: 'POST', body: { source: source.value } })
    }
    toast.add({ title: t('wf.saved'), color: 'success' })
    await navigateTo(back.value)
  } catch (e) {
    saveError(e, () => reloadNuxtApp())
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="grid gap-4 lg:h-[calc(100vh-9rem)] lg:min-h-[36rem] lg:grid-cols-2">
    <div class="flex flex-col lg:min-h-0">
      <div class="space-y-4 lg:min-h-0 lg:flex-1 lg:overflow-y-auto lg:pe-1">
        <div v-if="!loaded" class="p-4 text-sm text-(--ui-text-muted)">{{ t('common.loading') }}</div>
        <template v-else>
          <UFormField :label="t('wf.source')" :class="highlight && 'rounded-lg ring-2 ring-primary/60 ring-offset-2 ring-offset-(--ui-bg) transition'">
            <template #hint>
              <UTooltip :text="t('wf.sourceInfo')"><UIcon name="i-lucide-info" class="size-4 text-(--ui-text-muted)" /></UTooltip>
            </template>
            <UTextarea v-model="source" :rows="18" autoresize :maxrows="40" class="w-full font-mono text-xs" placeholder="---&#10;key: …&#10;name: …&#10;roles: …&#10;---" />
          </UFormField>
          <UAlert v-if="error" color="warning" variant="subtle" icon="i-lucide-triangle-alert" :title="t('wf.invalid')" :description="error" />
          <div v-else-if="def" class="rounded-lg border border-(--ui-border) p-3">
            <WorkflowPreview :def="def" />
          </div>
        </template>
      </div>
      <div class="flex justify-end gap-2 border-t border-(--ui-border) pt-3">
        <UButton color="neutral" variant="ghost" :label="t('common.close')" :to="back" />
        <UButton icon="i-lucide-save" :loading="saving" :disabled="!isAdmin || !source.trim() || !!error" :label="t('auto.save')" @click="save()" />
      </div>
    </div>
    <div class="h-[32rem] lg:h-auto lg:min-h-0">
      <ChatPanel :project-id="chatProjectId" purpose="workflow" :page-context="pageContext" @workflow-patch="applyPatch" @history="replay" />
    </div>
  </div>
</template>
