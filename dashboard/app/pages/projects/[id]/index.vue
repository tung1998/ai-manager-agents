<script setup lang="ts">
import type { Attachment } from '~/components/PromptInput.vue'
const route = useRoute()
const toast = useToast()
const { isAdmin } = useAuth()
const id = computed(() => route.params.id as string)

const { data, refresh } = await useFetch<{ project: Project }>(() => `/api/projects/${id.value}`)
const { data: tplData } = await useFetch<{ templates: OrgModel[] }>('/api/templates')
const project = computed(() => data.value?.project)
const templates = computed(() => tplData.value?.templates ?? [])

// the tab lives in the URL so the sidebar can link to each section;
// "Cấu hình" holds the org model and Skills & MCP (older links used tab=model|tools)
type Tab = 'chat' | 'tasks' | 'ops' | 'config'
const tabs: Tab[] = ['chat', 'tasks', 'ops', 'config']
const configSection = computed<'model' | 'skill' | 'mcp'>({
  get: () => route.query.tab === 'tools' ? 'skill' : (['skill', 'mcp'] as const).find(v => v === route.query.section) ?? 'model',
  set: v => navigateTo({ query: { tab: 'config', section: v } }, { replace: true })
})
const descOpen = ref(false)
// office's own source: approved changes take effect after "Cập nhật office"
const { data: updData } = useFetch<{ source?: { root: string } }>('/api/system/update', { lazy: true, immediate: isAdmin.value })
const isOfficeSource = computed(() => !!project.value?.path && updData.value?.source?.root === project.value.path)

// "Hỏi agent" from Vận hành: open Chat with the log attached
// send=true ("Sửa lỗi") starts a new conversation and sends right away
const chatPrefill = useState<{ text: string, files: Attachment[], send?: boolean } | null>('chat-prefill', () => null)
function askAgent(text: string, files: Attachment[], send = false) {
  chatPrefill.value = { text, files, send }
  tab.value = 'chat'
}
const tab = computed<Tab>({
  get: () => route.query.tab === 'model' || route.query.tab === 'tools' ? 'config' : tabs.find(t => t === route.query.tab) ?? 'chat',
  set: t => navigateTo({ query: { tab: t } }, { replace: true })
})
const { touch } = useProjectUsage()
watch(id, v => touch(v), { immediate: true })

// ---- apply / change model ----
const applyOpen = ref(false)
const templateId = ref('')
const applying = ref(false)
function openApply() {
  templateId.value = templates.value.find(t => t.id === project.value?.model?.source_template_id)?.id ?? templates.value[0]?.id ?? ''
  applyOpen.value = true
}
async function apply() {
  applying.value = true
  try {
    await $fetch(`/api/projects/${id.value}/model`, { method: 'POST', body: { template_id: templateId.value, replace: !!project.value?.model } })
    applyOpen.value = false
    await refresh()
    toast.add({ title: 'Đã áp mô hình', color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    applying.value = false
  }
}

// ---- edit project ----
const editOpen = ref(false)
const form = reactive({ name: '', description: '' })
function openEdit() {
  Object.assign(form, { name: project.value!.name, description: project.value!.description })
  editOpen.value = true
}
async function saveRepo() {
  await $fetch(`/api/projects/${id.value}`, { method: 'PATCH', body: { ...form } })
  editOpen.value = false
  await refresh()
}
async function removeRepo() {
  if (!confirm(`Bỏ quản lý project "${project.value?.name}"? File trong thư mục không bị ảnh hưởng.`)) return
  await $fetch(`/api/projects/${id.value}`, { method: 'DELETE' })
  await navigateTo('/projects')
}

function exportModel() {
  if (project.value?.model) window.open(`/api/org-models/${project.value.model.id}/export`, '_blank')
}

// Save the project's customised model as a reusable template.
async function saveAsTemplate() {
  const m = project.value?.model
  if (!m) return
  const key = prompt('Key cho mô hình (chữ thường, gạch ngang):', `${project.value!.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')}-${m.key}`)
  if (!key) return
  try {
    const res = await $fetch<{ model: OrgModel }>('/api/templates', { method: 'POST', body: { source_id: m.id, key, name: `${m.name} (${project.value!.name})` } })
    toast.add({ title: 'Đã lưu vào Mô hình', color: 'success', actions: [{ label: 'Mở', onClick: () => { navigateTo(`/templates/${res.model.id}`) } }] })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
</script>

<template>
  <PageShell :title="project?.name ?? 'Project'">
    <template #actions>
      <template v-if="project && isAdmin">
        <UButton v-if="!project.model" :to="`/projects/${id}/setup`" size="sm" icon="i-lucide-sparkles" label="Thiết lập bằng AI" />
        <UDropdownMenu
          :content="{ align: 'end' }"
          :items="[[
            { label: 'Sửa tên, mô tả', icon: 'i-lucide-pencil', onSelect: openEdit },
            { label: 'Thiết lập bằng AI', icon: 'i-lucide-sparkles', to: `/projects/${id}/setup` },
            { label: project.model ? 'Đổi mô hình' : 'Chọn mô hình', icon: 'i-lucide-network', onSelect: openApply }
          ], [
            { label: 'Tải JSON mô hình', icon: 'i-lucide-download', disabled: !project.model, onSelect: exportModel },
            { label: 'Lưu mô hình để dùng lại', icon: 'i-lucide-bookmark-plus', disabled: !project.model, onSelect: saveAsTemplate }
          ], [
            { label: 'Bỏ quản lý project', icon: 'i-lucide-folder-minus', color: 'error', onSelect: removeRepo }
          ]]"
        >
          <UButton icon="i-lucide-ellipsis" color="neutral" variant="ghost" aria-label="Thao tác project" />
        </UDropdownMenu>
      </template>
    </template>

    <div v-if="project" class="space-y-4">
      <!-- one quiet line of context -->
      <div class="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-xs text-(--ui-text-muted)">
        <span v-if="project.scope === 'folder'" class="flex min-w-0 items-center gap-1.5">
          <UIcon name="i-lucide-folder" class="size-3.5 shrink-0" /><span class="truncate font-mono">{{ project.path }}</span>
        </span>
        <span v-else class="flex items-center gap-1.5"><UIcon name="i-lucide-monitor" class="size-3.5" /> Helper toàn máy</span>
        <span v-if="project.git_remote" class="flex min-w-0 items-center gap-1.5">
          <UIcon name="i-lucide-git-branch" class="size-3.5 shrink-0" /><span class="truncate font-mono">{{ project.git_remote }}</span>
        </span>
        <UBadge v-if="!project.exists" label="Không tìm thấy thư mục" color="error" variant="subtle" size="sm" />
        <button
          v-if="project.description" type="button" class="min-w-0 basis-full text-left hover:text-(--ui-text)"
          :class="descOpen ? '' : 'truncate'" :title="descOpen ? '' : project.description" @click="descOpen = !descOpen"
        >
          {{ project.description }}
        </button>
      </div>

      <UAlert
        v-if="isOfficeSource" color="info" variant="subtle" icon="i-lucide-package" title="Đây là mã nguồn của chính office"
        description="Thay đổi được duyệt chỉ có hiệu lực sau khi build lại. Vào Cập nhật office để build, khởi động lại và tự quay về bản cũ nếu lỗi."
        :actions="[{ label: 'Cập nhật office', to: '/admin/update', icon: 'i-lucide-refresh-cw', color: 'info', variant: 'outline' }]"
      />

      <UTabs
        v-model="tab" :content="false" variant="link" class="w-full"
        :items="[
          { label: 'Chat', value: 'chat', icon: 'i-lucide-messages-square' },
          { label: 'Việc', value: 'tasks', icon: 'i-lucide-list-todo' },
          { label: 'Vận hành', value: 'ops', icon: 'i-lucide-activity' },
          { label: 'Cấu hình', value: 'config', icon: 'i-lucide-settings-2' }
        ]"
      />

      <OpsPanel v-if="tab === 'ops'" :project-id="project.id" :has-folder="!!project.path" @ask-agent="askAgent" />
      <template v-else-if="tab === 'config'">
        <SegmentedNav
          v-model="configSection"
          :items="[
            { value: 'model', label: 'Mô hình', icon: 'i-lucide-network' },
            ...(isAdmin ? [{ value: 'skill', label: 'Skills', icon: 'i-lucide-sparkles' }, { value: 'mcp', label: 'MCP', icon: 'i-lucide-plug-zap' }] : [])
          ]"
        />
        <ToolsPanel v-if="configSection !== 'model'" :key="configSection" :kind="configSection" :project-path="project.path" />
        <OrgModelEditor v-else-if="project.model" :key="project.model.id" :model-id="project.model.id" @changed="refresh()" />
        <NoModel v-else :project-id="id" :admin="isAdmin" @choose="openApply" />
      </template>
      <template v-else-if="project.model">
        <ChatPanel v-if="tab === 'chat'" :project-id="project.id" />
        <TaskPanel v-else :project-id="project.id" :model-kind="project.model.kind" :governance="project.model.governance.mode" />
      </template>
      <NoModel v-else :project-id="id" :admin="isAdmin" @choose="openApply" />
    </div>

    <UModal v-model:open="applyOpen" :title="project?.model ? 'Đổi mô hình' : 'Chọn mô hình'" :ui="{ content: 'max-w-xl' }">
      <template #body>
        <div class="space-y-4">
          <UAlert
            v-if="project?.model" color="warning" variant="subtle" icon="i-lucide-triangle-alert"
            description="Mô hình hiện tại của project và mọi chỉnh sửa trên nó sẽ bị thay thế. Tải JSON hoặc lưu để dùng lại trước nếu muốn giữ. Bản cũ cũng vẫn nằm trong Lịch sử."
          />
          <TemplatePicker v-model="templateId" :templates="templates" />
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="applyOpen = false" />
          <UButton :loading="applying" :disabled="!templateId" label="Áp dụng" @click="apply" />
        </div>
      </template>
    </UModal>

    <UModal v-model:open="editOpen" title="Sửa project">
      <template #body>
        <form id="project-edit" class="space-y-4" @submit.prevent="saveRepo">
          <UFormField label="Tên"><UInput v-model="form.name" class="w-full" /></UFormField>
          <UFormField label="Mô tả"><UTextarea v-model="form.description" :rows="3" class="w-full" /></UFormField>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="editOpen = false" />
          <UButton type="submit" form="project-edit" label="Lưu" />
        </div>
      </template>
    </UModal>
  </PageShell>
</template>
