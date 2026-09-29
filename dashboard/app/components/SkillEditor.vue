<script setup lang="ts">
// Writing a skill (.claude/skills/<name>/SKILL.md): the editor on one side,
// its own chat on the other; the chat's ```skill blocks fill the editor and
// nothing is written until Save. name "" = a new skill of the project.
const props = defineProps<{ projectId: string, projectPath: string, name?: string, scope?: 'project' | 'user' }>()
const toast = useToast()
const { t } = useLang()
const editing = computed(() => !!props.name)
const scope = computed(() => props.scope ?? 'project')

const form = reactive({ name: props.name ?? '', description: '', body: '' })
const extraMeta = ref<string[]>([]) // frontmatter lines office does not edit (kept as they are)
const otherFiles = ref<Record<string, string>>({}) // the skill's other files, kept
const loaded = ref(!editing.value)

// "---\nname: x\ndescription: y\n---\nbody"
function parse(md: string) {
  const m = md.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/)
  if (!m) return { meta: {} as Record<string, string>, extra: [] as string[], body: md }
  const meta: Record<string, string> = {}
  const extra: string[] = []
  for (const line of m[1]!.split('\n')) {
    const kv = line.match(/^(name|description):\s*(.*)$/)
    if (kv) meta[kv[1]!] = kv[2]!.replace(/^["']|["']$/g, '')
    else if (line.trim()) extra.push(line)
  }
  return { meta, extra, body: m[2]!.replace(/^\n+/, '') }
}
const quote = (s: string) => /[:#\n]|^\s|\s$/.test(s) ? JSON.stringify(s) : s
const skillMd = () => ['---', `name: ${form.name}`, `description: ${quote(form.description.trim())}`, ...extraMeta.value, '---', '', form.body.trim(), ''].join('\n')

onMounted(async () => {
  if (!editing.value) return
  try {
    const inv = await $fetch<{ items: AutoItem[] }>('/api/automation/scan')
    const item = inv.items.find(i => i.kind === 'skill' && i.name === props.name && i.location.type === scope.value &&
      (scope.value === 'user' || i.location.project_path === props.projectPath))
    if (!item) throw new Error(t('skill.notFound'))
    const it = await $fetch<LibraryItem>('/api/automation/content', { method: 'POST', body: refOf(item) })
    const files = { ...(it.files ?? {}) }
    const p = parse(files['SKILL.md'] ?? '')
    delete files['SKILL.md']
    otherFiles.value = files
    form.description = p.meta.description ?? item.description
    form.body = p.body
    extraMeta.value = p.extra
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    loaded.value = true
  }
})

// the chat next to it fills the editor
const highlight = ref<string[]>([])
function applyPatch(p: Record<string, unknown>) {
  const changed: string[] = []
  for (const k of ['name', 'description', 'body'] as const) {
    const v = p[k]
    if (typeof v !== 'string' || (k === 'name' && editing.value)) continue // a saved skill keeps its name (its folder)
    form[k] = k === 'name' ? v.toLowerCase().replace(/[^a-z0-9._-]+/g, '-').slice(0, 64) : v
    changed.push(k)
  }
  if (!changed.length) return
  highlight.value = changed
  setTimeout(() => { highlight.value = [] }, 4000)
  toast.add({ title: t('auto.filled', { n: changed.length }), color: 'info' })
}
const pageContext = () => JSON.stringify({ page: editing.value ? 'skill.edit' : 'skill.new', scope: scope.value, draft: { ...form }, other_files: Object.keys(otherFiles.value) })
const hl = (k: string) => highlight.value.includes(k) ? 'rounded-lg ring-2 ring-primary/60 ring-offset-2 ring-offset-(--ui-bg) transition' : ''

// Save: written through the same install as the library (its safety check
// asks before anything risky); an edit replaces the old one (kept in the trash)
const saving = ref(false)
const findings = ref<Finding[]>([])
async function save(accept = false) {
  if (!form.name.trim() || !form.description.trim()) return toast.add({ title: t('skill.needNameDesc'), color: 'error' })
  saving.value = true
  try {
    await $fetch('/api/automation/install', {
      method: 'POST',
      body: {
        kind: 'skill', name: form.name.trim(), overwrite: editing.value, accept,
        target: { scope: scope.value, project_path: scope.value === 'user' ? '' : props.projectPath },
        files: { ...otherFiles.value, 'SKILL.md': skillMd() }
      }
    })
    toast.add({ title: t('skill.saved'), color: 'success' })
    await navigateTo({ path: `/projects/${props.projectId}`, query: { tab: 'skill' } })
  } catch (e) {
    const data = (e as { data?: { code?: string, findings?: Finding[] } }).data
    if (data?.code === 'consent' && data.findings) {
      findings.value = data.findings
      if (confirm(t('skill.consent', { n: data.findings.length }))) return save(true)
    } else {
      if (data?.findings) findings.value = data.findings
      toast.add({ title: apiError(e), color: 'error' })
    }
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
          <UFormField :label="t('skill.name')" :help="editing ? t('skill.nameFixed') : t('skill.nameHelp')" required :class="hl('name')">
            <UInput
              :model-value="form.name" class="w-full font-mono" placeholder="tra-don" :disabled="editing"
              @update:model-value="(v: string | number) => { form.name = String(v).toLowerCase().replace(/[^a-z0-9._-]+/g, '-').slice(0, 64) }"
            >
              <template #leading><span class="font-mono text-xs text-(--ui-text-muted)">/</span></template>
            </UInput>
          </UFormField>
          <UFormField :label="t('skill.description')" :help="t('skill.descriptionHelp')" required :class="hl('description')">
            <UTextarea v-model="form.description" :rows="2" autoresize class="w-full" :placeholder="t('skill.descriptionPlaceholder')" />
          </UFormField>
          <UFormField :label="t('skill.body')" :help="t('skill.bodyHelp')" :class="hl('body')">
            <UTextarea v-model="form.body" :rows="18" autoresize class="w-full font-mono text-xs" :placeholder="t('skill.bodyPlaceholder')" />
          </UFormField>
          <p v-if="Object.keys(otherFiles).length" class="text-xs text-(--ui-text-muted)">
            {{ t('skill.otherFiles', { files: Object.keys(otherFiles).join(', ') }) }}
          </p>
          <UAlert v-if="findings.length" color="warning" variant="subtle" icon="i-lucide-shield-alert" :title="t('skill.findings')">
            <template #description>
              <ul class="list-disc ps-4 text-xs">
                <li v-for="(f, i) in findings" :key="i">{{ f.message }}<span v-if="f.line"> ({{ t('skill.line', { n: f.line }) }})</span></li>
              </ul>
            </template>
          </UAlert>
        </template>
      </div>
      <div class="flex justify-end gap-2 border-t border-(--ui-border) pt-3">
        <UButton color="neutral" variant="ghost" :label="t('org.form.close')" :to="{ path: `/projects/${projectId}`, query: { tab: 'skill' } }" />
        <UButton icon="i-lucide-save" :loading="saving" :label="t('auto.save')" @click="save()" />
      </div>
    </div>
    <div class="h-[32rem] lg:h-auto lg:min-h-0">
      <ChatPanel :project-id="projectId" purpose="skill" :page-context="pageContext" @skill-patch="applyPatch" />
    </div>
  </div>
</template>
