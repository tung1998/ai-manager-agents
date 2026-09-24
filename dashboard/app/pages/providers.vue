<script setup lang="ts">
const toast = useToast()
const route = useRoute()
const { isAdmin } = useAuth()
// Set when another page (project setup) sent the user here to connect AI first.
const next = computed(() => {
  const n = route.query.next
  return typeof n === 'string' && n.startsWith('/') && !n.startsWith('//') ? n : ''
})

const { data, refresh } = await useFetch<{ providers: Provider[] }>('/api/providers')
const { data: kindsData } = await useFetch<{ kinds: ProviderKind[] }>('/api/provider-kinds')
const providers = computed(() => data.value?.providers ?? [])
const kinds = computed(() => kindsData.value?.kinds ?? [])
const kindOf = (k: string) => kinds.value.find(x => x.kind === k)

// ---- form ----
const formOpen = ref(false)
const editing = ref<Provider | null>(null)
const form = reactive({
  name: '', kind: 'anthropic', base_url: '', api_key: '', api_key_env: '', keyMode: 'paste' as 'paste' | 'env',
  tier_models: { strong: '', balanced: '', fast: '' } as Record<ModelTier, string>
})
const formError = ref('')
const saving = ref(false)
const selectedKind = computed(() => kindOf(form.kind))
const commonKinds = computed(() => kinds.value.filter(k => k.common))
const otherKinds = computed(() => kinds.value.filter(k => !k.common))
const advancedOpen = ref(false)
const cliTool = computed(() => ({ claude_cli: 'claude', codex_cli: 'codex' } as Record<string, 'claude' | 'codex'>)[form.kind])
const cliReady = ref(true)
watch(cliTool, (v) => { if (!v) cliReady.value = true })
const norm = (u: string) => u.trim().replace(/\/+$/, '')
const urlChangedWithStoredKey = computed(() => !!editing.value?.has_api_key && form.keyMode === 'paste' && !form.api_key
  && norm(form.base_url) !== norm(editing.value.base_url))
const keyUrl = computed(() => ({ anthropic: 'https://console.anthropic.com', openai: 'https://platform.openai.com/api-keys' } as Record<string, string>)[form.kind] ?? '')
const kindIconOf = (k: string) => ({
  claude_cli: 'i-lucide-terminal', codex_cli: 'i-lucide-terminal', anthropic: 'i-lucide-key-round',
  openai: 'i-lucide-key-round', openai_compatible: 'i-lucide-server'
} as Record<string, string>)[k] ?? 'i-lucide-plug'

// Pick sensible defaults for a kind: its label as name, and the env var when one is already set.
function applyKindDefaults(k: string) {
  const info = kindOf(k)
  form.name = info?.label ?? ''
  form.base_url = ''
  form.api_key = ''
  form.api_key_env = info?.detected?.env_key ?? ''
  form.keyMode = info?.detected?.env_key ? 'env' : 'paste'
  form.tier_models = { strong: '', balanced: '', fast: '', ...info?.tier_models }
}

function openCreate() {
  editing.value = null
  advancedOpen.value = false
  // Prefer what already works on this machine: an installed Claude Code, then a key in the environment.
  const preferred = commonKinds.value.find(k => k.detected?.installed) ?? commonKinds.value.find(k => k.detected?.env_key) ?? commonKinds.value[0]
  form.kind = preferred?.kind ?? 'claude_cli'
  applyKindDefaults(form.kind)
  formError.value = ''
  formOpen.value = true
}

function openEdit(p: Provider) {
  editing.value = p
  Object.assign(form, {
    name: p.name, kind: p.kind, base_url: p.base_url, api_key: '', api_key_env: p.api_key_env,
    keyMode: p.api_key_env ? 'env' : 'paste'
  })
  form.tier_models = { strong: '', balanced: '', fast: '', ...p.tier_models }
  formError.value = ''
  advancedOpen.value = false
  formOpen.value = true
}

watch(() => form.kind, (k) => {
  if (!editing.value) applyKindDefaults(k)
})

async function save() {
  formError.value = ''
  saving.value = true
  const body: Record<string, unknown> = {
    name: form.name,
    kind: form.kind,
    base_url: form.base_url,
    api_key_env: form.keyMode === 'env' ? form.api_key_env : '',
    tier_models: Object.fromEntries(Object.entries(form.tier_models).filter(([, v]) => v))
  }
  // Only send the key when typed: omitting it keeps the stored one.
  if (form.keyMode === 'paste' && form.api_key) body.api_key = form.api_key
  if (form.keyMode === 'env' && editing.value?.has_api_key) body.api_key = ''
  try {
    const res = editing.value
      ? await $fetch<{ provider: Provider }>(`/api/providers/${editing.value.id}`, { method: 'PATCH', body })
      : await $fetch<{ provider: Provider }>('/api/providers', { method: 'POST', body })
    formOpen.value = false
    await refresh()
    toast.add({ title: editing.value ? 'Đã lưu kết nối' : 'Đã thêm kết nối', description: 'Đang kiểm tra kết nối…', color: 'success' })
    await runTest(res.provider)
  } catch (e) {
    formError.value = apiError(e)
  } finally {
    saving.value = false
  }
}

// ---- test ----
const testing = ref<string | null>(null)
const testOpen = ref(false)
const testTarget = ref<Provider | null>(null)
const testPrompt = ref('Chào bạn, hãy giới thiệu ngắn gọn bạn là model nào.')
const testModel = ref('')
const testResult = ref<{ ok: boolean, detail: string, models: string[], response?: { text: string, model: string, input_tokens: number, output_tokens: number, cost_usd?: number, duration_ms: number } } | null>(null)

async function runTest(p: Provider, prompt = '') {
  testing.value = p.id
  try {
    const res = await $fetch<typeof testResult.value>(`/api/providers/${p.id}/test`, {
      method: 'POST', body: { prompt, model: prompt ? testModel.value : '' }
    })
    if (!prompt && res!.ok && next.value) {
      toast.add({ title: `${p.name}: kết nối tốt`, description: 'Quay lại thiết lập project…', color: 'success' })
      await navigateTo(next.value)
      return
    }
    if (prompt) {
      testResult.value = res
    } else {
      toast.add({ title: res!.ok ? `${p.name}: kết nối tốt` : `${p.name}: lỗi kết nối`, description: res!.detail, color: res!.ok ? 'success' : 'error' })
    }
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    testing.value = null
  }
}

function openPromptTest(p: Provider) {
  testTarget.value = p
  testModel.value = p.tier_models.balanced || ''
  testResult.value = null
  testOpen.value = true
}

async function setDefault(p: Provider) {
  await $fetch(`/api/providers/${p.id}/default`, { method: 'POST' })
  await refresh()
}

async function remove(p: Provider) {
  if (!confirm(`Xóa kết nối "${p.name}"? Agent đang dùng sẽ chuyển sang kết nối mặc định.`)) return
  try {
    await $fetch(`/api/providers/${p.id}`, { method: 'DELETE' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

function menu(p: Provider) {
  return [
    [
      { label: 'Gửi thử prompt', icon: 'i-lucide-message-square', onSelect: () => openPromptTest(p) },
      { label: 'Sửa', icon: 'i-lucide-pencil', onSelect: () => openEdit(p) },
      { label: 'Đặt làm mặc định', icon: 'i-lucide-star', disabled: p.is_default, onSelect: () => setDefault(p) }
    ],
    [{ label: 'Xóa', icon: 'i-lucide-trash', color: 'error' as const, onSelect: () => remove(p) }]
  ]
}

const statusColor = (s: string) => (s === 'ok' ? 'success' : s === 'error' ? 'error' : 'neutral')
const statusText = (s: string) => (s === 'ok' ? 'Hoạt động' : s === 'error' ? 'Lỗi' : 'Chưa kiểm tra')
</script>

<template>
  <PageShell title="Kết nối AI">
    <template #actions>
      <UButton v-if="isAdmin" icon="i-lucide-plus" label="Thêm kết nối" @click="openCreate" />
    </template>

    <div class="space-y-4">
      <UAlert
        v-if="next" color="primary" variant="subtle" icon="i-lucide-sparkles"
        title="Kết nối AI để thiết lập project"
        description="Thiết lập bằng AI cần ít nhất một kết nối hoạt động. Thêm hoặc kiểm tra kết nối, bạn sẽ được đưa quay lại."
        :actions="providers.some(p => p.status === 'ok') ? [{ label: 'Tiếp tục thiết lập', to: next, icon: 'i-lucide-arrow-right' }] : []"
      />


      <div v-if="!providers.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
        <UIcon name="i-lucide-plug" class="mx-auto size-8 text-(--ui-text-dimmed)" />
        <p class="mt-2 font-medium">Chưa có kết nối nào</p>
        <p class="text-sm text-(--ui-text-muted)">Thêm Claude API, OpenAI API, hoặc dùng Claude Code CLI đã đăng nhập trên máy.</p>
        <UButton v-if="isAdmin" class="mt-4" icon="i-lucide-plus" label="Thêm kết nối" @click="openCreate" />
      </div>

      <div class="grid gap-4 lg:grid-cols-2">
        <UCard v-for="p in providers" :key="p.id">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <p class="truncate font-semibold">{{ p.name }}</p>
                <UBadge v-if="p.is_default" label="Mặc định" icon="i-lucide-star" size="sm" variant="subtle" />
              </div>
              <p class="text-sm text-(--ui-text-muted)">{{ kindOf(p.kind)?.label ?? p.kind }}</p>
            </div>
            <div class="flex items-center gap-1">
              <UBadge :label="statusText(p.status)" :color="statusColor(p.status)" variant="subtle" />
              <UButton
                v-if="isAdmin" icon="i-lucide-refresh-cw" color="neutral" variant="ghost" size="sm"
                :loading="testing === p.id" title="Kiểm tra kết nối" @click="runTest(p)"
              />
              <UDropdownMenu v-if="isAdmin" :items="menu(p)">
                <UButton icon="i-lucide-ellipsis-vertical" color="neutral" variant="ghost" size="sm" />
              </UDropdownMenu>
            </div>
          </div>

          <dl class="mt-4 grid grid-cols-3 gap-2 text-sm">
            <div v-for="t in (['strong', 'balanced', 'fast'] as const)" :key="t" class="rounded-md bg-(--ui-bg-muted) px-2 py-1.5">
              <dt class="text-xs text-(--ui-text-muted)">{{ modelTierLabel[t] }}</dt>
              <dd class="truncate font-mono text-xs">{{ p.tier_models[t] || '—' }}</dd>
            </div>
          </dl>

          <div class="mt-3 space-y-1 text-xs text-(--ui-text-muted)">
            <p v-if="p.api_key_env">Key từ biến môi trường <code>{{ p.api_key_env }}</code></p>
            <p v-else-if="p.has_api_key">API key đã lưu (mã hóa) <code>{{ p.api_key_hint }}</code></p>
            <p v-if="p.base_url">{{ kindOf(p.kind)?.is_cli ? 'Binary' : 'URL' }}: <code>{{ p.base_url }}</code></p>
            <p v-if="p.status_detail" :class="p.status === 'error' ? 'text-(--ui-error)' : ''">{{ p.status_detail }}</p>
          </div>
        </UCard>
      </div>
    </div>

    <!-- create / edit -->
    <UModal v-model:open="formOpen" :title="editing ? `Sửa ${editing.name}` : 'Thêm kết nối AI'" :ui="{ content: 'max-w-2xl' }">
      <template #body>
        <form id="provider-form" class="space-y-5" @submit.prevent="save">
          <!-- kind -->
          <div v-if="!editing" class="flex flex-wrap items-center gap-2">
            <button
              v-for="k in commonKinds" :key="k.kind" type="button"
              class="flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm transition"
              :class="form.kind === k.kind ? 'border-(--ui-primary) bg-(--ui-primary)/10 text-(--ui-primary)' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
              @click="form.kind = k.kind"
            >
              <UIcon :name="kindIconOf(k.kind)" class="size-4" />
              {{ k.label }}
              <span v-if="k.detected?.installed || k.detected?.env_key" class="size-1.5 rounded-full bg-(--ui-success)" title="Có sẵn trên máy" />
            </button>
            <USelect
              :model-value="otherKinds.some(k => k.kind === form.kind) ? form.kind : undefined"
              :items="otherKinds.map(k => ({ label: k.label, value: k.kind }))"
              placeholder="Khác…" size="sm" class="w-40"
              @update:model-value="(v: string) => form.kind = v"
            />
          </div>
          <div v-else class="flex items-center gap-2 text-sm">
            <UIcon :name="kindIconOf(form.kind)" class="size-5 text-primary" />
            <span class="font-medium">{{ selectedKind?.label }}</span>
          </div>

          <CliSetup v-if="cliTool" :key="cliTool" :tool="cliTool" @ready="(v: boolean) => cliReady = v" />

          <UFormField label="Tên hiển thị" required>
            <UInput v-model="form.name" class="w-full" />
          </UFormField>

          <!-- endpoint for compatible APIs is not optional, so it is not hidden -->
          <UFormField v-if="form.kind === 'openai_compatible'" label="Địa chỉ API" required help="Ví dụ Ollama: http://localhost:11434/v1 · OpenRouter: https://openrouter.ai/api/v1">
            <UInput v-model="form.base_url" :placeholder="selectedKind?.base_url_hint" class="w-full font-mono" />
          </UFormField>

          <!-- API key -->
          <template v-if="!selectedKind?.is_cli">
            <UFormField label="API key" :required="selectedKind?.needs_key">
              <UTabs
                v-model="form.keyMode"
                :items="[{ label: 'Dán key', value: 'paste' }, { label: 'Đọc từ biến môi trường', value: 'env' }]"
                :content="false" size="xs" class="mb-2"
              />
              <UInput
                v-if="form.keyMode === 'paste'" v-model="form.api_key" type="password" autocomplete="off" class="w-full"
                :placeholder="editing?.has_api_key ? `Đã lưu ${editing.api_key_hint} — để trống để giữ nguyên` : 'Dán API key vào đây'"
              />
              <UInput v-else v-model="form.api_key_env" placeholder="ANTHROPIC_API_KEY" class="w-full font-mono" />
              <p v-if="urlChangedWithStoredKey" class="mt-1 text-xs text-(--ui-warning)">
                Bạn đã đổi địa chỉ API: nhập lại key. Key đã lưu không bao giờ được gửi tới địa chỉ mới.
              </p>
              <template #help>
                <span v-if="form.keyMode === 'paste'">Key được mã hóa trước khi lưu và không bao giờ hiển thị lại.</span>
                <span v-else>Office đọc key từ biến môi trường của máy chạy server, không lưu key.</span>
                <template v-if="keyUrl"> Lấy key tại <a :href="keyUrl" target="_blank" class="underline">{{ keyUrl.replace('https://', '') }}</a>.</template>
              </template>
            </UFormField>
          </template>

          <!-- advanced -->
          <UCollapsible v-model:open="advancedOpen">
            <UButton
              color="neutral" variant="link" size="sm" class="px-0"
              :icon="advancedOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" label="Nâng cao (không bắt buộc)"
            />
            <template #content>
              <div class="mt-3 space-y-4 rounded-lg border border-(--ui-border) p-4">
                <UFormField
                  v-if="selectedKind?.is_cli" label="Đường dẫn tới lệnh"
                  :help="`Chỉ cần điền khi lệnh ${selectedKind.base_url_hint} không chạy được từ terminal, VD /opt/homebrew/bin/${selectedKind.base_url_hint}. Để trống là dùng mặc định.`"
                >
                  <UInput v-model="form.base_url" :placeholder="selectedKind?.base_url_hint" class="w-full font-mono" />
                </UFormField>
                <UFormField
                  v-else-if="form.kind !== 'openai_compatible'" label="Địa chỉ API"
                  help="Để trống là dùng địa chỉ chính thức. Chỉ đổi khi đi qua proxy hoặc cổng riêng của công ty."
                >
                  <UInput v-model="form.base_url" :placeholder="selectedKind?.base_url_hint" class="w-full font-mono" />
                </UFormField>

                <div>
                  <p class="text-sm font-medium">Model theo mức</p>
                  <p class="mt-0.5 text-xs text-(--ui-text-muted)">
                    Agent chỉ chọn mức <b>Mạnh</b> (việc khó, tổng hợp), <b>Cân bằng</b> (phân tích thường ngày) hoặc <b>Nhanh</b> (lấy dữ liệu, việc lặp lại).
                    Ở đây quy định mỗi mức dùng model nào. Đã điền sẵn giá trị khuyên dùng; để trống thì office tự chọn sau khi kiểm tra kết nối.
                  </p>
                  <div class="mt-2 grid gap-3 sm:grid-cols-3">
                    <UFormField v-for="t in (['strong', 'balanced', 'fast'] as const)" :key="t" :label="modelTierLabel[t]">
                      <UInput v-model="form.tier_models[t]" :list="`models-${t}`" class="w-full font-mono" size="sm" />
                      <datalist :id="`models-${t}`">
                        <option v-for="m in editing?.models ?? []" :key="m" :value="m" />
                      </datalist>
                    </UFormField>
                  </div>
                </div>
              </div>
            </template>
          </UCollapsible>

          <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="formOpen = false" />
          <UButton type="submit" form="provider-form" :loading="saving" :disabled="!cliReady" :label="editing ? 'Lưu và kiểm tra' : 'Thêm và kiểm tra'" />
        </div>
      </template>
    </UModal>

    <!-- prompt test -->
    <UModal v-model:open="testOpen" :title="`Gửi thử: ${testTarget?.name ?? ''}`" :ui="{ content: 'max-w-xl' }">
      <template #body>
        <div class="space-y-4">
          <UFormField label="Model">
            <UInput v-model="testModel" list="test-models" class="w-full font-mono" />
            <datalist id="test-models">
              <option v-for="m in testTarget?.models ?? []" :key="m" :value="m" />
            </datalist>
          </UFormField>
          <UFormField label="Prompt">
            <UTextarea v-model="testPrompt" :rows="3" class="w-full" />
          </UFormField>
          <UAlert v-if="testResult && !testResult.ok" color="error" variant="subtle" :description="testResult.detail" />
          <div v-if="testResult?.response" class="rounded-md bg-(--ui-bg-muted) p-3 text-sm">
            <p class="whitespace-pre-wrap">{{ testResult.response.text }}</p>
            <p class="mt-2 text-xs text-(--ui-text-muted)">
              {{ testResult.response.model }} · {{ testResult.response.input_tokens }} in / {{ testResult.response.output_tokens }} out
              · {{ testResult.response.duration_ms }} ms
              <span v-if="testResult.response.cost_usd"> · ${{ testResult.response.cost_usd.toFixed(4) }}</span>
            </p>
          </div>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Đóng" @click="testOpen = false" />
          <UButton icon="i-lucide-send" label="Gửi" :loading="testing === testTarget?.id" @click="testTarget && runTest(testTarget, testPrompt)" />
        </div>
      </template>
    </UModal>
  </PageShell>
</template>
