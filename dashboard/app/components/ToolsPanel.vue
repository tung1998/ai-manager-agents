<script setup lang="ts">
import type { DropdownMenuItem } from '@nuxt/ui'
import type { InstallSource } from './InstallModal.vue'

// Without `projectPath` the panel manages everything on the machine (Thư viện
// page). With it ('' = machine-wide helper project) it shows what that project
// gets and installs only there.
const props = defineProps<{ kind: ItemKind, projectPath?: string }>()
const scoped = computed(() => props.projectPath !== undefined)
const fixedTarget = computed(() => (scoped.value ? (props.projectPath || '__user') : undefined))
const toast = useToast()
const { inv, loading, scan } = useInventory()
const meta = computed(() => kindMeta[props.kind])

const tabs = computed(() => [
  { label: scoped.value ? 'Đang dùng' : 'Đã cài', value: 'installed', icon: 'i-lucide-hard-drive' },
  { label: scoped.value ? 'Thêm từ thư viện' : 'Thư viện', value: 'library', icon: 'i-lucide-library' },
  ...(props.kind === 'mcp'
    ? [{ label: 'Phổ biến', value: 'catalog', icon: 'i-lucide-star' }, { label: 'Tìm MCP', value: 'search', icon: 'i-lucide-search' }]
    : [])
])
const tab = ref('installed')

// ---- installed ----
const q = ref('')
const where = ref('__all')
const items = computed(() => (inv.value?.items ?? []).filter((i) => {
  if (i.kind !== props.kind) return false
  if (!scoped.value) return true
  // a project sees its own items, its private (local) MCP, and machine-wide ones
  return i.location.type === 'user' || (!!props.projectPath && i.location.project_path === props.projectPath && (i.location.type === 'project' || i.location.type === 'local'))
}))
const whereOptions = computed(() => {
  const labels = new Map<string, string>()
  for (const i of items.value) labels.set(locKey(i.location), i.location.label)
  return [{ label: 'Mọi nơi', value: '__all' }, ...[...labels].map(([value, label]) => ({ label, value }))]
})
function locKey(l: AutoLocation) {
  return l.type === 'project' || l.type === 'local' ? `${l.type}:${l.project_path}` : l.type === 'plugin' ? `plugin:${l.label}` : l.type
}
const groups = computed(() => {
  const needle = q.value.trim().toLowerCase()
  const map = new Map<string, { location: AutoLocation, items: AutoItem[] }>()
  for (const i of items.value) {
    if (where.value !== '__all' && locKey(i.location) !== where.value) continue
    if (needle && !`${i.name} ${i.description}`.toLowerCase().includes(needle)) continue
    const k = locKey(i.location)
    if (!map.has(k)) map.set(k, { location: scoped.value && i.location.type === 'user' ? { ...i.location, label: 'Kế thừa từ toàn máy' } : i.location, items: [] })
    map.get(k)!.items.push(i)
  }
  const order: AutoLocation['type'][] = ['user', 'project', 'local', 'plugin', 'cursor', 'claude_desktop', 'codex']
  return [...map.values()].sort((a, b) => order.indexOf(a.location.type) - order.indexOf(b.location.type) || a.location.label.localeCompare(b.location.label))
})

function itemMenu(i: AutoItem): DropdownMenuItem[][] {
  const main: DropdownMenuItem[] = [
    { label: 'Xem nội dung', icon: 'i-lucide-eye', onSelect: () => view(i) },
    { label: scoped.value ? 'Chép vào project này' : 'Cài vào nơi khác', icon: 'i-lucide-copy', disabled: !canCopy(i), onSelect: () => startInstall({ kind: i.kind, name: i.name, from: i }) },
    { label: 'Lưu vào thư viện', icon: 'i-lucide-library', disabled: i.location.type === 'codex', onSelect: () => saveToLibrary(i) }
  ]
  // in a project, machine-wide items are removed from the Thư viện page
  const removable = i.location.editable && !(scoped.value && i.location.type === 'user')
  return removable ? [main, [{ label: 'Gỡ', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => remove(i) }]] : [main]
}

// ---- viewer ----
const viewOpen = ref(false)
const viewing = ref<LibraryItem | null>(null)
async function view(i: AutoItem) {
  try {
    viewing.value = await $fetch<LibraryItem>('/api/automation/content', { method: 'POST', body: refOf(i) })
    viewOpen.value = true
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
const viewFiles = computed(() => {
  const v = viewing.value
  if (!v) return []
  if (v.template) return [{ name: 'config.json', content: JSON.stringify(v.template.config, null, 2) }]
  return Object.entries(v.files ?? {}).sort(([a], [b]) => (a === 'SKILL.md' ? -1 : b === 'SKILL.md' ? 1 : a.localeCompare(b))).map(([name, content]) => ({ name, content }))
})

async function saveToLibrary(i: AutoItem) {
  try {
    await $fetch('/api/automation/save-to-library', { method: 'POST', body: { ref: refOf(i), name: '' } })
    toast.add({ title: `Đã lưu ${i.name} vào thư viện`, description: i.kind === 'mcp' ? 'Token/giá trị bí mật được thay bằng ô cần điền khi cài.' : undefined, color: 'success' })
    refreshLib()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

async function remove(i: AutoItem) {
  const msg = i.kind === 'mcp'
    ? `Gỡ MCP "${i.name}" khỏi ${i.location.label}?`
    : `Gỡ "${i.name}" khỏi ${i.location.label}? Thư mục được chuyển vào thùng rác của office.`
  if (!confirm(msg)) return
  try {
    await $fetch('/api/automation/remove', { method: 'POST', body: refOf(i) })
    toast.add({ title: `Đã gỡ ${i.name}`, color: 'success' })
    scan()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

// ---- install ----
const installOpen = ref(false)
const installSource = ref<InstallSource | null>(null)
function startInstall(s: InstallSource) {
  installSource.value = s
  installOpen.value = true
}

// ---- library ----
const { data: libData, refresh: refreshLib } = await useFetch<{ items: LibraryItem[] }>(() => `/api/automation/library/${props.kind}`)
const library = computed(() => libData.value?.items ?? [])

const editOpen = ref(false)
const editing = ref<{ isNew: boolean, name: string, content: string, description: string, files: Record<string, string> }>({ isNew: true, name: '', content: '', description: '', files: {} })
const editError = ref('')
const editFindings = ref<Finding[]>([])

const starter: Record<ItemKind, string> = {
  skill: '---\nname: ten-skill\ndescription: Dùng khi ... (mô tả ngắn để Claude biết lúc nào nên dùng)\n---\n\n# Hướng dẫn\n\n1. ...\n',
  agent: '---\nname: ten-agent\ndescription: Agent chuyên ... Dùng khi ...\ntools: Read, Grep, Glob\n---\n\nBạn là ...\n',
  mcp: '{\n  "type": "stdio",\n  "command": "npx",\n  "args": ["-y", "ten-goi-mcp"],\n  "env": { "API_KEY": "{{API_KEY}}" }\n}\n'
}

async function openEdit(item?: LibraryItem) {
  editError.value = ''
  editFindings.value = []
  if (!item) {
    editing.value = { isNew: true, name: '', content: starter[props.kind], description: '', files: {} }
  } else {
    const full = await $fetch<LibraryItem>(`/api/automation/library/${props.kind}/${item.name}`)
    const files = full.files ?? {}
    editing.value = {
      isNew: false,
      name: full.name,
      description: full.template?.description ?? '',
      files,
      content: props.kind === 'mcp' ? JSON.stringify(full.template?.config ?? {}, null, 2) : props.kind === 'skill' ? files['SKILL.md'] ?? '' : Object.values(files)[0] ?? ''
    }
  }
  editOpen.value = true
}

// {{KEY}} placeholders in an MCP config become inputs
function inputsFrom(cfg: Record<string, unknown>): TemplateInput[] {
  const out: TemplateInput[] = []
  const walk = (v: unknown, kind: TemplateInput['kind']) => {
    if (typeof v === 'string') {
      for (const m of v.matchAll(/\{\{([A-Za-z0-9_]+)\}\}/g)) {
        if (!out.some(i => i.key === m[1])) out.push({ key: m[1]!, label: m[1]!, kind, secret: /key|token|secret|password|auth/i.test(m[1]!), required: true })
      }
    } else if (Array.isArray(v)) v.forEach(e => walk(e, kind))
    else if (v && typeof v === 'object') for (const [k, e] of Object.entries(v)) walk(e, k === 'env' ? 'env' : k === 'headers' ? 'header' : kind)
  }
  walk(cfg, 'arg')
  return out
}

async function saveEdit() {
  const e = editing.value
  editError.value = ''
  let body: Record<string, unknown>
  if (props.kind === 'mcp') {
    let cfg: Record<string, unknown>
    try {
      cfg = JSON.parse(e.content)
    } catch {
      editError.value = 'Cấu hình không phải JSON hợp lệ'
      return
    }
    body = { template: { name: e.name, title: e.name, description: e.description, config: cfg, inputs: inputsFrom(cfg) } }
  } else if (props.kind === 'skill') {
    body = { files: { ...e.files, 'SKILL.md': e.content } }
  } else {
    body = { files: { [`${e.name}.md`]: e.content } }
  }
  try {
    await $fetch(`/api/automation/library/${props.kind}/${e.name.trim()}`, { method: 'PUT', body })
    editOpen.value = false
    toast.add({ title: `Đã lưu ${e.name}`, color: 'success' })
    refreshLib()
  } catch (err) {
    editError.value = apiError(err)
    editFindings.value = (err as { data?: { findings?: Finding[] } }).data?.findings ?? []
  }
}

async function removeLib(item: LibraryItem) {
  if (!confirm(`Xóa "${item.name}" khỏi thư viện?`)) return
  try {
    await $fetch(`/api/automation/library/${props.kind}/${item.name}`, { method: 'DELETE' })
    refreshLib()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

async function installLib(item: LibraryItem) {
  if (props.kind === 'mcp') {
    const full = await $fetch<LibraryItem>(`/api/automation/library/mcp/${item.name}`)
    startInstall({ kind: 'mcp', name: item.name, library: item.name, template: full.template })
  } else {
    startInstall({ kind: props.kind, name: item.name, library: item.name })
  }
}

// ---- MCP catalog & registry ----
const installedNames = computed(() => new Set(items.value.map(i => i.name)))
// in a project, copying from the machine means "install here"
const canCopy = (i: AutoItem) => i.location.type !== 'codex' && !(scoped.value && i.location.type !== 'user')
const { data: catalogData } = await useFetch<{ items: MCPTemplate[] }>('/api/automation/mcp/catalog', { immediate: props.kind === 'mcp' })
const catalog = computed(() => catalogData.value?.items ?? [])
const regQ = ref('')
const regItems = ref<MCPTemplate[]>([])
const regBusy = ref(false)
const regError = ref('')
const regSearched = ref(false)
async function searchRegistry() {
  if (!regQ.value.trim()) return
  regBusy.value = true
  regError.value = ''
  try {
    regItems.value = (await $fetch<{ items: MCPTemplate[] }>('/api/automation/mcp/registry', { query: { q: regQ.value } })).items
    regSearched.value = true
  } catch (e) {
    regError.value = apiError(e)
  } finally {
    regBusy.value = false
  }
}
async function saveTemplate(t: MCPTemplate) {
  try {
    await $fetch(`/api/automation/library/mcp/${t.name}`, { method: 'PUT', body: { template: { ...t, source: undefined, id: undefined } } })
    toast.add({ title: `Đã lưu ${t.title} vào thư viện`, color: 'success' })
    refreshLib()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
const summary = (t: MCPTemplate) => {
  const c = t.config as { url?: string, command?: string, args?: string[] }
  return c.url ?? [c.command, ...(c.args ?? [])].join(' ')
}
</script>

<template>
  <div>
    <div class="space-y-4">
      <div class="flex flex-wrap items-center gap-2">
        <UTabs v-model="tab" :items="tabs" :content="false" size="sm" variant="link" />
        <div class="ms-auto flex gap-2">
          <UButton v-if="tab === 'library' && !scoped" size="sm" icon="i-lucide-plus" label="Tạo mới" @click="openEdit()" />
          <UButton size="sm" color="neutral" variant="outline" icon="i-lucide-scan-search" label="Quét lại" :loading="loading" @click="scan" />
        </div>
      </div>

      <!-- installed -->
      <template v-if="tab === 'installed'">
        <div class="flex flex-wrap gap-2">
          <UInput v-model="q" icon="i-lucide-search" placeholder="Tìm theo tên, mô tả" class="w-64" />
          <USelect v-if="!scoped" v-model="where" :items="whereOptions" class="w-64" />
          <span class="self-center text-sm text-(--ui-text-muted)">{{ groups.reduce((a, g) => a + g.items.length, 0) }} mục</span>
        </div>
        <div v-if="loading && !inv" class="py-10 text-center text-(--ui-text-muted)">Đang quét máy…</div>
        <div v-else-if="!groups.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
          <UIcon :name="meta.icon" class="mx-auto size-8 text-(--ui-text-dimmed)" />
          <p class="mt-2 font-medium">Chưa thấy {{ meta.label.toLowerCase() }} nào</p>
          <p class="text-sm text-(--ui-text-muted)">
            {{ kind === 'mcp' ? 'Chọn tab Phổ biến hoặc Tìm MCP để cài.' : 'Tạo trong tab Thư viện rồi cài vào máy hoặc project.' }}
          </p>
        </div>
        <section v-for="g in groups" :key="locKey(g.location)" class="space-y-2">
          <h3 class="flex items-center gap-2 text-sm font-semibold">
            <UIcon :name="locationIcon[g.location.type]" class="text-(--ui-text-muted)" />
            {{ g.location.label }}
            <span class="font-normal text-(--ui-text-muted)">· {{ g.items.length }}</span>
            <UBadge v-if="!g.location.editable" color="neutral" variant="subtle" size="sm" label="chỉ xem" />
            <NuxtLink v-if="g.location.project_id" :to="`/projects/${g.location.project_id}`" class="text-xs font-normal text-(--ui-primary)">mở project</NuxtLink>
          </h3>
          <div class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
            <div v-for="i in g.items" :key="i.location.path + i.name" class="flex items-center gap-3 px-4 py-2.5">
              <UIcon :name="meta.icon" class="size-4 shrink-0 text-primary" />
              <div class="min-w-0 flex-1">
                <p class="truncate font-mono text-sm font-medium">{{ i.name }}</p>
                <p v-if="i.description" class="line-clamp-1 text-xs text-(--ui-text-muted)">{{ i.description }}</p>
                <p v-else-if="i.meta?.url || i.meta?.command" class="truncate font-mono text-xs text-(--ui-text-muted)">
                  {{ i.meta?.transport }} · {{ i.meta?.url ?? i.meta?.command }}
                </p>
              </div>
              <UBadge v-if="i.meta?.tools" color="neutral" variant="subtle" size="sm" :label="i.meta.tools" class="hidden max-w-48 truncate md:inline-flex" />
              <UDropdownMenu :items="itemMenu(i)" :content="{ align: 'end' }">
                <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Thao tác" />
              </UDropdownMenu>
            </div>
          </div>
        </section>
      </template>

      <!-- library -->
      <template v-else-if="tab === 'library'">
        <p v-if="!scoped" class="text-sm text-(--ui-text-muted)">
          Bộ sưu tập của office. Lưu từ máy hoặc tạo mới, rồi cài vào toàn máy hay từng project.
        </p>
        <div v-if="!library.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
          <UIcon name="i-lucide-library" class="mx-auto size-8 text-(--ui-text-dimmed)" />
          <p class="mt-2 font-medium">Thư viện trống</p>
          <p class="text-sm text-(--ui-text-muted)">
            <template v-if="scoped">Thêm vào thư viện ở trang <NuxtLink to="/library" class="text-(--ui-primary)">Thư viện</NuxtLink>.</template>
            <template v-else>Ở tab Đã cài, chọn "Lưu vào thư viện", hoặc bấm Tạo mới.</template>
          </p>
        </div>
        <div v-else class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
          <div v-for="l in library" :key="l.name" class="flex items-center gap-3 px-4 py-2.5">
            <UIcon :name="meta.icon" class="size-4 shrink-0 text-primary" />
            <div class="min-w-0 flex-1">
              <p class="truncate font-mono text-sm font-medium">{{ l.name }}</p>
              <p v-if="l.description" class="line-clamp-1 text-xs text-(--ui-text-muted)">{{ l.description }}</p>
            </div>
            <UButton size="sm" icon="i-lucide-download" label="Cài" @click="installLib(l)" />
            <template v-if="!scoped">
              <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-pencil" aria-label="Sửa" @click="openEdit(l)" />
              <UButton size="sm" color="error" variant="ghost" icon="i-lucide-trash-2" aria-label="Xóa" @click="removeLib(l)" />
            </template>
          </div>
        </div>
      </template>

      <!-- catalog -->
      <template v-else-if="tab === 'catalog'">
        <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          <div v-for="t in catalog" :key="t.id" class="flex flex-col gap-2 rounded-lg border border-(--ui-border) p-4">
            <div class="flex items-center gap-2">
              <p class="font-medium">{{ t.title }}</p>
              <UBadge v-if="t.category" color="neutral" variant="subtle" size="sm" :label="t.category" />
              <UBadge v-if="installedNames.has(t.name)" color="success" variant="subtle" size="sm" icon="i-lucide-check" label="đã cài" class="ms-auto" />
            </div>
            <p class="text-sm text-(--ui-text-muted)">{{ t.description }}</p>
            <code class="truncate text-xs text-(--ui-text-dimmed)">{{ summary(t) }}</code>
            <div class="mt-auto flex items-center gap-2 pt-1">
              <UButton size="sm" icon="i-lucide-download" label="Cài" @click="startInstall({ kind: 'mcp', name: t.name, template: t })" />
              <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-library" label="Lưu" @click="saveTemplate(t)" />
              <UButton v-if="t.homepage" size="sm" color="neutral" variant="link" :to="t.homepage" target="_blank" icon="i-lucide-external-link" aria-label="Trang chủ" class="ms-auto" />
            </div>
          </div>
        </div>
      </template>

      <!-- registry search -->
      <template v-else-if="tab === 'search'">
        <form class="flex gap-2" @submit.prevent="searchRegistry">
          <UInput v-model="regQ" icon="i-lucide-search" placeholder="Tìm trong MCP Registry, ví dụ: postgres, slack, figma" class="flex-1" autofocus />
          <UButton type="submit" label="Tìm" :loading="regBusy" />
        </form>
        <p class="text-xs text-(--ui-text-muted)">
          Nguồn: registry.modelcontextprotocol.io (MCP Registry chính thức). Server do cộng đồng đăng, hãy xem kỹ trước khi cài.
        </p>
        <UAlert v-if="regError" color="error" variant="subtle" icon="i-lucide-circle-alert" :title="regError" />
        <p v-if="regSearched && !regItems.length" class="py-6 text-center text-(--ui-text-muted)">Không có kết quả cài được.</p>
        <div class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)" :class="{ hidden: !regItems.length }">
          <div v-for="t in regItems" :key="t.id" class="flex items-start gap-3 px-4 py-3">
            <div class="min-w-0 flex-1">
              <p class="font-medium">{{ t.title }}</p>
              <p class="line-clamp-2 text-sm text-(--ui-text-muted)">{{ t.description }}</p>
              <code class="block truncate text-xs text-(--ui-text-dimmed)">{{ summary(t) }}</code>
            </div>
            <UButton v-if="t.homepage" size="sm" color="neutral" variant="ghost" :to="t.homepage" target="_blank" icon="i-lucide-external-link" aria-label="Trang chủ" />
            <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-library" aria-label="Lưu vào thư viện" @click="saveTemplate(t)" />
            <UButton size="sm" icon="i-lucide-download" label="Cài" @click="startInstall({ kind: 'mcp', name: t.name, template: t })" />
          </div>
        </div>
      </template>
    </div>

    <InstallModal v-model:open="installOpen" :source="installSource" :fixed-target="fixedTarget" @done="scan" />

    <UModal v-model:open="viewOpen" :title="viewing?.name ?? ''" :ui="{ content: 'max-w-3xl' }">
      <template #body>
        <div class="space-y-3">
          <p v-if="viewing?.kind === 'mcp'" class="text-xs text-(--ui-text-muted)">Giá trị bí mật đã được che.</p>
          <div v-for="f in viewFiles" :key="f.name">
            <p class="mb-1 font-mono text-xs text-(--ui-text-muted)">{{ f.name }}</p>
            <pre class="max-h-96 overflow-auto rounded-md bg-(--ui-bg-elevated) p-3 text-xs whitespace-pre-wrap">{{ f.content }}</pre>
          </div>
        </div>
      </template>
    </UModal>

    <UModal v-model:open="editOpen" :title="editing.isNew ? `Tạo ${meta.label.toLowerCase()}` : `Sửa ${editing.name}`" :ui="{ content: 'max-w-3xl' }">
      <template #body>
        <form id="lib-form" class="space-y-4" @submit.prevent="saveEdit">
          <UFormField label="Tên" help="Chữ, số, dấu chấm, gạch ngang">
            <UInput v-model="editing.name" :disabled="!editing.isNew" class="w-full font-mono" required />
          </UFormField>
          <UFormField v-if="kind === 'mcp'" label="Mô tả">
            <UInput v-model="editing.description" class="w-full" />
          </UFormField>
          <UFormField
            :label="kind === 'skill' ? 'SKILL.md' : kind === 'agent' ? 'Nội dung agent (.md)' : 'Cấu hình (JSON)'"
            :help="kind === 'mcp' ? 'Dùng {{TÊN}} cho giá trị cần điền khi cài (token, API key). Không ghi token thật vào thư viện.' : undefined"
          >
            <UTextarea v-model="editing.content" :rows="16" autoresize class="w-full font-mono text-xs" />
          </UFormField>
          <p v-if="kind === 'skill' && Object.keys(editing.files).length > 1" class="text-xs text-(--ui-text-muted)">
            Giữ nguyên {{ Object.keys(editing.files).length - 1 }} file khác trong skill.
          </p>
          <UAlert v-if="editError" color="error" variant="subtle" icon="i-lucide-circle-alert" :title="editError">
            <template v-if="editFindings.length" #description>
              <p v-for="(f, i) in editFindings" :key="i">{{ f.message }} · dòng {{ f.line }}</p>
            </template>
          </UAlert>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="editOpen = false" />
          <UButton type="submit" form="lib-form" label="Lưu" />
        </div>
      </template>
    </UModal>
  </div>
</template>
