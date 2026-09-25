<script setup lang="ts">
const { isAdmin } = useAuth()
const { data, refresh } = await useFetch<{ projects: Project[] }>('/api/projects')
const { data: tplData } = await useFetch<{ templates: OrgModel[] }>('/api/templates')
const { data: sys } = await useFetch<{ mode: string, home_dir: string, project_root?: string }>('/api/system')
const projects = computed(() => data.value?.projects ?? [])
const templates = computed(() => tplData.value?.templates ?? [])

const addOpen = ref(false)
const form = reactive({ scope: 'folder' as 'folder' | 'machine', path: '', name: '', template_id: '' })
const error = ref('')
const adding = ref(false)

function openAdd(path = '', name = '') {
  Object.assign(form, { scope: 'folder', path, name, template_id: '__ai' })
  error.value = ''
  addOpen.value = true
}

// folders Claude Code has opened on this machine that office does not manage yet
const found = ref<MachineProject[]>([])
const scanning = ref(false)
async function scanMachine() {
  scanning.value = true
  try {
    const inv = await $fetch<Inventory>('/api/automation/scan')
    const known = new Set(projects.value.map(p => p.path))
    found.value = inv.projects.filter(p => p.exists && !p.project_id && !known.has(p.path))
  } catch {
    found.value = []
  } finally {
    scanning.value = false
  }
}
onMounted(() => { if (isAdmin.value) scanMachine() })

async function add() {
  adding.value = true
  error.value = ''
  try {
    const res = await $fetch<{ project: Project }>('/api/projects', {
      method: 'POST',
      body: { path: form.scope === 'folder' ? form.path : '', name: form.name, template_id: form.template_id === '__ai' ? '' : form.template_id }
    })
    addOpen.value = false
    await navigateTo(form.template_id === '__ai' ? `/projects/${res.project.id}/setup` : `/projects/${res.project.id}`)
  } catch (e) {
    error.value = apiError(e)
  } finally {
    adding.value = false
  }
  await refresh()
}
</script>

<template>
  <PageShell title="Project">
    <template #actions>
      <UButton v-if="isAdmin" icon="i-lucide-folder-plus" label="Thêm project" @click="openAdd()" />
    </template>

    <div class="space-y-4">
      <UAlert
        v-if="sys" color="neutral" variant="subtle" icon="i-lucide-hard-drive"
        :title="sys.mode === 'local' ? 'Office cài trong một project' : 'Office cài trên máy'"
        :description="sys.mode === 'local'
          ? `Cài trong ${sys.project_root}, dữ liệu ở ${sys.home_dir}.`
          : `Quản lý project ở bất kỳ đâu trên máy, dữ liệu ở ${sys.home_dir}.`"
      />

      <div v-if="!projects.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
        <UIcon name="i-lucide-folder-git-2" class="mx-auto size-8 text-(--ui-text-dimmed)" />
        <p class="mt-2 font-medium">Chưa có project nào</p>
        <p class="text-sm text-(--ui-text-muted)">
          Chọn một thư mục trên máy, hoặc tạo helper cho toàn bộ máy. Cũng có thể chạy <code>office init</code> trong thư mục.
        </p>
        <UButton v-if="isAdmin" class="mt-4" icon="i-lucide-folder-plus" label="Thêm project" @click="openAdd()" />
      </div>

      <div class="grid gap-3">
        <NuxtLink
          v-for="p in projects" :key="p.id" :to="`/projects/${p.id}`"
          class="flex flex-wrap items-center gap-4 rounded-lg border border-(--ui-border) p-4 transition hover:border-(--ui-primary)"
        >
          <UIcon :name="p.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder-git-2'" class="size-6 text-primary" />
          <div class="min-w-0 flex-1">
            <p class="font-semibold">{{ p.name }}</p>
            <p v-if="p.scope === 'folder'" class="truncate font-mono text-xs text-(--ui-text-muted)">{{ p.path }}</p>
            <p v-else class="text-xs text-(--ui-text-muted)">Helper toàn máy, không gắn thư mục</p>
          </div>
          <UBadge v-if="!p.exists" label="Không tìm thấy thư mục" color="error" variant="subtle" />
          <div v-if="p.model" class="flex items-center gap-2 text-sm">
            <UIcon :name="kindIcon[p.model.kind]" class="size-4" />
            {{ p.model.name }} · {{ p.model.agent_count }} agent
          </div>
          <UBadge v-else label="Chưa có mô hình" color="warning" variant="subtle" />
          <UIcon name="i-lucide-chevron-right" class="size-4 text-(--ui-text-dimmed)" />
        </NuxtLink>
      </div>

      <section v-if="isAdmin && (found.length || scanning)" class="space-y-2">
        <div class="flex items-center gap-2">
          <h3 class="text-sm font-semibold">Tìm thấy trên máy</h3>
          <span class="text-xs text-(--ui-text-muted)">Thư mục Claude Code đã mở, chưa quản lý trong office</span>
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-refresh-cw" :loading="scanning" aria-label="Quét lại" class="ms-auto" @click="scanMachine" />
        </div>
        <div class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
          <div v-for="f in found" :key="f.path" class="flex flex-wrap items-center gap-3 px-4 py-2.5">
            <UIcon name="i-lucide-folder" class="size-4 text-(--ui-text-muted)" />
            <div class="min-w-0 flex-1">
              <p class="font-medium">{{ f.name }}</p>
              <p class="truncate font-mono text-xs text-(--ui-text-muted)">{{ f.path }}</p>
            </div>
            <div class="flex flex-wrap gap-1">
              <UBadge v-if="f.skills" color="neutral" variant="subtle" size="sm" icon="i-lucide-sparkles" :label="`${f.skills} skill`" />
              <UBadge v-if="f.agents" color="neutral" variant="subtle" size="sm" icon="i-lucide-bot" :label="`${f.agents} agent`" />
              <UBadge v-if="f.mcp" color="neutral" variant="subtle" size="sm" icon="i-lucide-plug-zap" :label="`${f.mcp} MCP`" />
              <UBadge v-if="f.claude_md" color="neutral" variant="subtle" size="sm" label="CLAUDE.md" />
              <UBadge v-if="f.agents_md" color="neutral" variant="subtle" size="sm" label="AGENTS.md" />
            </div>
            <UButton size="sm" variant="outline" icon="i-lucide-plus" label="Thêm" @click="openAdd(f.path, f.name)" />
          </div>
        </div>
      </section>
    </div>

    <UModal v-model:open="addOpen" title="Thêm project" :ui="{ content: 'max-w-2xl' }">
      <template #body>
        <form id="project-form" class="space-y-4" @submit.prevent="add">
          <UFormField label="Phạm vi">
            <div class="grid gap-2 sm:grid-cols-2">
              <button
                v-for="opt in [
                  { value: 'folder', icon: 'i-lucide-folder-git-2', title: 'Một thư mục', text: 'Quản lý một project cụ thể: sửa code, theo dõi hệ thống…' },
                  { value: 'machine', icon: 'i-lucide-monitor', title: 'Toàn bộ máy', text: 'Helper không gắn thư mục, làm việc ở bất kỳ đâu trên máy.' }
                ]" :key="opt.value" type="button"
                class="flex items-start gap-3 rounded-lg border p-3 text-left transition"
                :class="form.scope === opt.value ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
                @click="form.scope = opt.value as 'folder' | 'machine'"
              >
                <UIcon :name="opt.icon" class="mt-0.5 size-5 shrink-0 text-primary" />
                <div>
                  <p class="font-medium">{{ opt.title }}</p>
                  <p class="text-xs text-(--ui-text-muted)">{{ opt.text }}</p>
                </div>
              </button>
            </div>
          </UFormField>

          <UFormField v-if="form.scope === 'folder'" label="Thư mục" required>
            <FolderTree v-model="form.path" />
          </UFormField>

          <UFormField
            label="Tên" :required="form.scope === 'machine'"
            :help="form.scope === 'folder' ? 'Để trống để tự nhận từ package.json, go.mod hoặc tên thư mục' : undefined"
          >
            <UInput v-model="form.name" class="w-full" :placeholder="form.scope === 'machine' ? 'VD: Trợ lý máy' : ''" />
          </UFormField>

          <UFormField label="Mô hình tổ chức">
            <TemplatePicker v-model="form.template_id" :templates="templates" allow-ai />
          </UFormField>
          <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="addOpen = false" />
          <UButton
            type="submit" form="project-form" :loading="adding" label="Thêm project"
            :disabled="form.scope === 'folder' ? !form.path : !form.name"
          />
        </div>
      </template>
    </UModal>
  </PageShell>
</template>
