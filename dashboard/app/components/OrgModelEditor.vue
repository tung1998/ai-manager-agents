<script setup lang="ts">
const props = defineProps<{ modelId: string }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToast()
const { isAdmin } = useAuth()

const { data, refresh } = await useFetch<{ model: OrgModel }>(() => `/api/org-models/${props.modelId}`)
const { data: provData } = await useFetch<{ providers: Provider[] }>('/api/providers')
const model = computed(() => data.value?.model)
const agents = computed(() => model.value?.agents ?? [])
const providers = computed(() => provData.value?.providers ?? [])
const defaultProvider = computed(() => providers.value.find(p => p.is_default))

const rows = computed(() => (['lead', 'manager', 'worker'] as const)
  .map(t => ({ tier: t, agents: agents.value.filter(a => a.tier === t) }))
  .filter(r => r.agents.length))

function providerOf(a: Agent) {
  return providers.value.find(p => p.id === a.provider_id) ?? defaultProvider.value
}
function resolvedModel(a: Agent) {
  return a.llm_model || providerOf(a)?.tier_models[a.model_tier] || '—'
}
function nameOf(key: string) {
  return agents.value.find(a => a.key === key)?.name ?? key
}

const historyOpen = ref(false)
const problems = ref<string[]>([])
function showError(e: unknown) {
  const d = (e as { data?: { error?: string, problems?: string[] } })?.data
  problems.value = d?.problems ?? []
  if (!d?.problems) toast.add({ title: d?.error ?? 'Có lỗi xảy ra', color: 'error' })
}

// ---- model settings ----
const settingsOpen = ref(false)
const settings = reactive({ name: '', description: '', kind: 'custom', mode: 'hierarchy', quorum: 2, veto: [] as string[], notes: '' })
function openSettings() {
  const m = model.value!
  Object.assign(settings, {
    name: m.name, description: m.description, kind: m.kind, mode: m.governance.mode || 'hierarchy',
    quorum: m.governance.quorum || 2, veto: [...(m.governance.veto ?? [])], notes: m.governance.notes ?? ''
  })
  problems.value = []
  settingsOpen.value = true
}
async function saveSettings() {
  try {
    await $fetch(`/api/org-models/${props.modelId}`, {
      method: 'PATCH',
      body: {
        name: settings.name, description: settings.description, kind: settings.kind,
        governance: {
          mode: settings.mode, notes: settings.notes, veto: settings.veto,
          quorum: settings.mode === 'council' ? Number(settings.quorum) : 0
        }
      }
    })
    settingsOpen.value = false
    await refresh()
    emit('changed')
    toast.add({ title: 'Đã lưu mô hình', color: 'success' })
  } catch (e) {
    showError(e)
  }
}
const leads = computed(() => agents.value.filter(a => a.tier === 'lead'))

// ---- agent editor ----
const editorOpen = ref(false)
const editing = ref<Agent | null>(null)
const empty = (): Omit<Agent, 'id' | 'org_model_id' | 'sort'> => ({
  key: '', name: '', tier: 'worker', role: '', description: '', reports_to: [], provider_id: '',
  model_tier: 'fast', llm_model: '', instructions: '', permissions: { read_only: true, tools: [], requires_approval: false }
})
const form = reactive(empty())
const saving = ref(false)

function openAgent(a: Agent) {
  editing.value = a
  const { id: _id, org_model_id: _org, sort: _sort, ...rest } = JSON.parse(JSON.stringify(a)) as Agent
  Object.assign(form, empty(), rest)
  form.permissions.tools ??= []
  problems.value = []
  editorOpen.value = true
}
function newAgent(tier: AgentTier) {
  editing.value = null
  Object.assign(form, empty(), { tier, model_tier: tier === 'lead' ? 'strong' : tier === 'manager' ? 'balanced' : 'fast' })
  if (tier !== 'lead' && leads.value[0]) form.reports_to = [leads.value[0].key]
  problems.value = []
  editorOpen.value = true
}

const bossOptions = computed(() => agents.value
  .filter(a => a.tier !== 'worker' && a.key !== form.key)
  .map(a => ({ label: `${a.name} (${a.key})`, value: a.key })))

// Select items cannot use an empty value, so "default provider" is a sentinel.
const DEFAULT_PROVIDER = '__default'
const providerChoice = computed({
  get: () => form.provider_id || DEFAULT_PROVIDER,
  set: (v: string) => { form.provider_id = v === DEFAULT_PROVIDER ? '' : v }
})
const providerOptions = computed(() => [
  { label: `Mặc định${defaultProvider.value ? ` (${defaultProvider.value.name})` : ''}`, value: DEFAULT_PROVIDER },
  ...providers.value.map(p => ({ label: p.name, value: p.id }))
])
const formProvider = computed(() => providers.value.find(p => p.id === form.provider_id) ?? defaultProvider.value)

async function saveAgent() {
  saving.value = true
  problems.value = []
  // Send only editable fields: the API rejects unknown ones such as id.
  const body = {
    key: form.key, name: form.name, tier: form.tier, role: form.role, description: form.description,
    reports_to: form.tier === 'lead' ? [] : form.reports_to, provider_id: form.provider_id,
    model_tier: form.model_tier, llm_model: form.llm_model, instructions: form.instructions, permissions: form.permissions
  }
  try {
    if (editing.value) {
      await $fetch(`/api/agents/${editing.value.id}`, { method: 'PATCH', body })
    } else {
      await $fetch(`/api/org-models/${props.modelId}/agents`, { method: 'POST', body })
    }
    editorOpen.value = false
    await refresh()
    emit('changed')
    toast.add({ title: `Đã lưu agent ${form.name}`, color: 'success' })
  } catch (e) {
    showError(e)
  } finally {
    saving.value = false
  }
}

async function deleteAgent() {
  if (!editing.value || !confirm(`Xóa agent "${editing.value.name}"?`)) return
  try {
    await $fetch(`/api/agents/${editing.value.id}`, { method: 'DELETE' })
    editorOpen.value = false
    await refresh()
    emit('changed')
  } catch (e) {
    showError(e)
  }
}

const tierColor: Record<AgentTier, 'primary' | 'info' | 'neutral'> = { lead: 'primary', manager: 'info', worker: 'neutral' }
</script>

<template>
  <div v-if="model" class="space-y-6">
    <!-- summary -->
    <UCard>
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div class="min-w-0 space-y-1">
          <div class="flex items-center gap-2">
            <UIcon :name="kindIcon[model.kind]" class="size-5 text-primary" />
            <h2 class="text-lg font-semibold">{{ model.name }}</h2>
            <UBadge :label="kindLabel[model.kind]" variant="subtle" />
            <UBadge v-if="model.builtin" label="Có sẵn" color="neutral" variant="outline" size="sm" />
          </div>
          <p class="max-w-3xl text-sm text-(--ui-text-muted)">{{ model.description }}</p>
          <p class="text-sm">
            <span class="text-(--ui-text-muted)">Ra quyết định:</span>
            {{ governanceLabel[model.governance.mode] ?? model.governance.mode }}
            <template v-if="model.governance.mode === 'council'"> · cần {{ model.governance.quorum }}/{{ leads.length }} phiếu</template>
            <template v-if="model.governance.veto?.length"> · phủ quyết: {{ model.governance.veto.map(nameOf).join(', ') }}</template>
          </p>
          <p v-if="model.governance.notes" class="text-xs text-(--ui-text-muted)">{{ model.governance.notes }}</p>
        </div>
        <div class="flex gap-2">
          <UButton icon="i-lucide-history" label="Lịch sử" color="neutral" variant="ghost" @click="historyOpen = true" />
        </div>
        <div v-if="isAdmin" class="flex gap-2">
          <UButton icon="i-lucide-settings-2" label="Cài đặt mô hình" color="neutral" variant="outline" @click="openSettings" />
          <UDropdownMenu :items="[[
            { label: 'Thêm lead', icon: 'i-lucide-crown', onSelect: () => newAgent('lead') },
            { label: 'Thêm manager', icon: 'i-lucide-briefcase', onSelect: () => newAgent('manager') },
            { label: 'Thêm worker', icon: 'i-lucide-wrench', onSelect: () => newAgent('worker') }
          ]]">
            <UButton icon="i-lucide-user-plus" label="Thêm agent" />
          </UDropdownMenu>
        </div>
      </div>
    </UCard>

    <UAlert
      v-if="problems.length && !editorOpen && !settingsOpen" color="error" variant="subtle" title="Mô hình không hợp lệ"
      :description="problems.join(' · ')"
    />

    <!-- org chart -->
    <div class="space-y-3">
      <template v-for="(row, i) in rows" :key="row.tier">
        <div class="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-(--ui-text-muted)">
          <span>{{ tierLabel[row.tier] }}</span>
          <span class="h-px flex-1 bg-(--ui-border)" />
        </div>
        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <button
            v-for="a in row.agents" :key="a.id" type="button"
            class="rounded-lg border border-(--ui-border) bg-(--ui-bg) p-3 text-left transition hover:border-(--ui-primary) hover:shadow-sm"
            @click="openAgent(a)"
          >
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <p class="truncate font-medium">{{ a.name }}</p>
                <p class="truncate font-mono text-xs text-(--ui-text-muted)">{{ a.key }}</p>
              </div>
              <UBadge :label="modelTierLabel[a.model_tier]" :color="tierColor[a.tier]" variant="subtle" size="sm" />
            </div>
            <p v-if="a.role" class="mt-2 line-clamp-2 text-sm text-(--ui-text-toned)">{{ a.role }}</p>
            <div class="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-(--ui-text-muted)">
              <span class="font-mono">{{ resolvedModel(a) }}</span>
              <UIcon v-if="a.permissions.read_only" name="i-lucide-eye" class="size-3.5" title="Chỉ đọc" />
              <UIcon v-else name="i-lucide-pencil" class="size-3.5 text-(--ui-warning)" title="Có thể thay đổi" />
              <UIcon v-if="a.permissions.requires_approval" name="i-lucide-shield-check" class="size-3.5" title="Cần người duyệt" />
            </div>
            <div v-if="a.reports_to.length" class="mt-2 flex flex-wrap gap-1">
              <UBadge v-for="r in a.reports_to" :key="r" :label="`↑ ${nameOf(r)}`" color="neutral" variant="outline" size="sm" />
            </div>
          </button>
        </div>
        <div v-if="i < rows.length - 1" class="flex justify-center">
          <UIcon name="i-lucide-chevrons-down" class="size-4 text-(--ui-text-dimmed)" />
        </div>
      </template>
    </div>

    <RevisionHistory v-model:open="historyOpen" :model-id="modelId" @restored="refresh(); emit('changed')" />

    <!-- model settings -->
    <UModal v-model:open="settingsOpen" title="Cài đặt mô hình" :ui="{ content: 'max-w-lg' }">
      <template #body>
        <form id="model-settings" class="space-y-4" @submit.prevent="saveSettings">
          <UFormField label="Tên" required>
            <UInput v-model="settings.name" class="w-full" />
          </UFormField>
          <UFormField label="Mô tả">
            <UTextarea v-model="settings.description" :rows="3" class="w-full" />
          </UFormField>
          <UFormField label="Loại">
            <USelect v-model="settings.kind" :items="Object.entries(kindLabel).map(([value, label]) => ({ label, value }))" class="w-full" />
          </UFormField>
          <UFormField label="Cách ra quyết định">
            <USelect v-model="settings.mode" :items="Object.entries(governanceLabel).map(([value, label]) => ({ label, value }))" class="w-full" />
          </UFormField>
          <UFormField v-if="settings.mode === 'council'" :label="`Số phiếu cần (tối đa ${leads.length})`">
            <UInputNumber v-model="settings.quorum" :min="1" :max="leads.length" />
          </UFormField>
          <UFormField label="Quyền phủ quyết" help="Lead có thể chặn hành động có side effect.">
            <USelectMenu v-model="settings.veto" multiple value-key="value" :items="leads.map(a => ({ label: a.name, value: a.key }))" class="w-full" />
          </UFormField>
          <UFormField label="Ghi chú quy trình">
            <UTextarea v-model="settings.notes" :rows="2" class="w-full" />
          </UFormField>
          <UAlert v-if="problems.length" color="error" variant="subtle" title="Chưa hợp lệ">
            <template #description>
              <ul class="list-disc ps-4">
                <li v-for="p in problems" :key="p">{{ p }}</li>
              </ul>
            </template>
          </UAlert>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="settingsOpen = false" />
          <UButton type="submit" form="model-settings" label="Lưu" />
        </div>
      </template>
    </UModal>

    <!-- agent editor -->
    <USlideover v-model:open="editorOpen" :title="editing ? editing.name : 'Agent mới'" :ui="{ content: 'max-w-xl' }">
      <template #body>
        <form id="agent-form" class="space-y-4" @submit.prevent="saveAgent">
          <UAlert v-if="problems.length" color="error" variant="subtle" title="Chưa hợp lệ">
            <template #description>
              <ul class="list-disc ps-4">
                <li v-for="p in problems" :key="p">{{ p }}</li>
              </ul>
            </template>
          </UAlert>
          <fieldset :disabled="!isAdmin" class="space-y-4">
            <div class="grid gap-3 sm:grid-cols-2">
              <UFormField label="Tên" required>
                <UInput v-model="form.name" class="w-full" />
              </UFormField>
              <UFormField label="Key" required help="chữ thường, số, gạch ngang">
                <UInput v-model="form.key" class="w-full font-mono" />
              </UFormField>
            </div>
            <div class="grid gap-3 sm:grid-cols-2">
              <UFormField label="Cấp">
                <USelect v-model="form.tier" :items="Object.entries(tierLabel).map(([value, label]) => ({ label, value }))" class="w-full" />
              </UFormField>
              <UFormField v-if="form.tier !== 'lead'" label="Báo cáo cho">
                <USelectMenu v-model="form.reports_to" multiple value-key="value" :items="bossOptions" class="w-full" />
              </UFormField>
            </div>
            <UFormField label="Vai trò">
              <UInput v-model="form.role" class="w-full" placeholder="VD: Thiết kế kỹ thuật" />
            </UFormField>
            <UFormField label="Mô tả">
              <UTextarea v-model="form.description" :rows="2" class="w-full" />
            </UFormField>

            <USeparator label="Model" />
            <div class="grid gap-3 sm:grid-cols-2">
              <UFormField label="Kết nối AI">
                <USelect v-model="providerChoice" :items="providerOptions" class="w-full" />
              </UFormField>
              <UFormField label="Hạng model">
                <USelect v-model="form.model_tier" :items="Object.entries(modelTierLabel).map(([value, label]) => ({ label, value }))" class="w-full" />
              </UFormField>
            </div>
            <UFormField
              label="Model cụ thể (tùy chọn)"
              :help="`Để trống sẽ dùng ${formProvider?.tier_models[form.model_tier] || 'model theo hạng của kết nối'}`"
            >
              <UInput v-model="form.llm_model" list="agent-models" class="w-full font-mono" />
              <datalist id="agent-models">
                <option v-for="m in formProvider?.models ?? []" :key="m" :value="m" />
              </datalist>
            </UFormField>

            <USeparator label="Hướng dẫn và quyền" />
            <UFormField label="Hướng dẫn (system prompt riêng)">
              <UTextarea v-model="form.instructions" :rows="6" class="w-full" autoresize />
            </UFormField>
            <div class="grid gap-3 sm:grid-cols-2">
              <USwitch v-model="form.permissions.read_only" label="Chỉ đọc" description="Không sửa code, dữ liệu" />
              <USwitch v-model="form.permissions.requires_approval" label="Cần người duyệt" description="Mọi side effect phải duyệt" />
            </div>
            <UFormField label="Công cụ được phép" help="VD: read, search, edit, shell:test, mcp:logs">
              <UInputTags v-model="form.permissions.tools" class="w-full" />
            </UFormField>
          </fieldset>

        </form>
      </template>
      <template #footer>
        <div class="flex w-full items-center justify-between gap-2">
          <UButton v-if="isAdmin && editing" color="error" variant="ghost" icon="i-lucide-trash" label="Xóa" @click="deleteAgent" />
          <span v-else />
          <div class="flex gap-2">
            <UButton color="neutral" variant="ghost" label="Đóng" @click="editorOpen = false" />
            <UButton v-if="isAdmin" type="submit" form="agent-form" :loading="saving" label="Lưu" />
          </div>
        </div>
      </template>
    </USlideover>
  </div>
</template>
