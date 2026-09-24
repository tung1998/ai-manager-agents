<script setup lang="ts">
const route = useRoute()
const toast = useToast()
const { isAdmin } = useAuth()
const id = computed(() => route.params.id as string)

const { data, refresh } = await useFetch<{ project: Project }>(() => `/api/projects/${id.value}`)
const { data: tplData } = await useFetch<{ templates: OrgModel[] }>('/api/templates')
const project = computed(() => data.value?.project)
const templates = computed(() => tplData.value?.templates ?? [])

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
      <UButton to="/projects" icon="i-lucide-arrow-left" label="Tất cả project" color="neutral" variant="ghost" />
    </template>

    <div v-if="project" class="space-y-6">
      <UCard>
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0 space-y-1">
            <p v-if="project.scope === 'folder'" class="font-mono text-sm">{{ project.path }}</p>
            <p v-else class="flex items-center gap-1.5 text-sm"><UIcon name="i-lucide-monitor" class="size-4" /> Helper toàn máy, không gắn thư mục</p>
            <p v-if="project.git_remote" class="font-mono text-xs text-(--ui-text-muted)">{{ project.git_remote }}</p>
            <p v-if="project.description" class="text-sm text-(--ui-text-muted)">{{ project.description }}</p>
            <UBadge v-if="!project.exists" label="Không tìm thấy thư mục trên máy" color="error" variant="subtle" />
          </div>
          <div v-if="isAdmin" class="flex flex-wrap gap-2">
            <UButton icon="i-lucide-pencil" label="Sửa" color="neutral" variant="outline" @click="openEdit" />
            <UButton :to="`/projects/${id}/setup`" icon="i-lucide-sparkles" label="Thiết lập bằng AI" />
            <UButton icon="i-lucide-network" :label="project.model ? 'Đổi mô hình' : 'Chọn mô hình'" color="neutral" variant="outline" @click="openApply" />
            <UDropdownMenu :items="[[
              { label: 'Tải JSON mô hình', icon: 'i-lucide-download', disabled: !project.model, onSelect: exportModel },
              { label: 'Lưu mô hình để dùng lại', icon: 'i-lucide-bookmark-plus', disabled: !project.model, onSelect: saveAsTemplate },
              { label: 'Bỏ quản lý project', icon: 'i-lucide-folder-minus', color: 'error', onSelect: removeRepo }
            ]]">
              <UButton icon="i-lucide-ellipsis-vertical" color="neutral" variant="ghost" />
            </UDropdownMenu>
          </div>
        </div>
      </UCard>

      <OrgModelEditor v-if="project.model" :key="project.model.id" :model-id="project.model.id" @changed="refresh()" />
      <div v-else class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
        <p class="font-medium">Project chưa có mô hình tổ chức</p>
        <p class="text-sm text-(--ui-text-muted)">Để AI quét project và đề xuất, hoặc tự chọn Solo, Team, Tam quyền phân lập.</p>
        <div v-if="isAdmin" class="mt-4 flex justify-center gap-2">
          <UButton :to="`/projects/${id}/setup`" icon="i-lucide-sparkles" label="Thiết lập bằng AI" />
          <UButton icon="i-lucide-network" label="Tự chọn mô hình" color="neutral" variant="outline" @click="openApply" />
        </div>
      </div>
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
