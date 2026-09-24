<script setup lang="ts">
interface Change { kind: 'provider' | 'template' | 'project', name: string, op: 'create' | 'update' | 'unchanged' | 'skip', detail?: string }

definePageMeta({ admin: true })
const toast = useToast()

// ---- import ----
const fileInput = ref<HTMLInputElement | null>(null)
const bundle = ref<unknown>(null)
const fileName = ref('')
const plan = ref<Change[] | null>(null)
const importing = ref(false)

async function onFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  fileName.value = f.name
  plan.value = null
  try {
    bundle.value = JSON.parse(await f.text())
    const res = await $fetch<{ changes: Change[] }>('/api/transfer/import', { method: 'POST', body: { bundle: bundle.value, dry_run: true } })
    plan.value = res.changes
  } catch (err) {
    bundle.value = null
    toast.add({ title: 'Không đọc được file', description: apiError(err, 'File không phải bundle config hợp lệ'), color: 'error' })
  } finally {
    if (fileInput.value) fileInput.value.value = ''
  }
}

const pending = computed(() => plan.value?.filter(c => c.op === 'create' || c.op === 'update').length ?? 0)

async function applyImport() {
  importing.value = true
  try {
    await $fetch('/api/transfer/import', { method: 'POST', body: { bundle: bundle.value, dry_run: false } })
    toast.add({ title: `Đã nhập ${pending.value} thay đổi`, color: 'success' })
    plan.value = null
    bundle.value = null
  } catch (err) {
    toast.add({ title: apiError(err), color: 'error' })
  } finally {
    importing.value = false
  }
}

// ---- backup ----
const backingUp = ref(false)
const backupPath = ref('')
async function backup() {
  backingUp.value = true
  try {
    backupPath.value = (await $fetch<{ path: string }>('/api/transfer/backup', { method: 'POST', body: {} })).path
  } catch (err) {
    toast.add({ title: apiError(err), color: 'error' })
  } finally {
    backingUp.value = false
  }
}

const opMeta: Record<string, { label: string, color: 'success' | 'info' | 'neutral' | 'warning' }> = {
  create: { label: 'Tạo mới', color: 'success' },
  update: { label: 'Cập nhật', color: 'info' },
  unchanged: { label: 'Giữ nguyên', color: 'neutral' },
  skip: { label: 'Bỏ qua', color: 'warning' }
}
const kindLabelOf: Record<string, string> = { provider: 'Kết nối AI', template: 'Mô hình mẫu', project: 'Project' }
</script>

<template>
  <PageShell title="Sao lưu & đồng bộ">
    <div class="max-w-4xl space-y-6">
      <UCard>
        <template #header>
          <p class="font-medium">Xuất config</p>
          <p class="text-sm text-(--ui-text-muted)">Kết nối AI (không kèm API key), mô hình mẫu, project và mô hình của từng project. Không gồm tài khoản.</p>
        </template>
        <div class="space-y-3">
          <UButton icon="i-lucide-download" label="Tải file config" to="/api/transfer/export" external target="_blank" />
          <p class="text-sm text-(--ui-text-muted)">Muốn đưa lên git để review và theo dõi thay đổi, dùng CLI: mỗi mẫu, mỗi project là một file.</p>
          <pre class="rounded-md bg-(--ui-bg-muted) p-3 text-xs">office export ./office-config --commit</pre>
        </div>
      </UCard>

      <UCard>
        <template #header>
          <p class="font-medium">Nhập config</p>
          <p class="text-sm text-(--ui-text-muted)">Chọn file đã xuất. Office cho xem trước thay đổi, chỉ ghi khi bạn bấm Áp dụng. Bản cũ của mô hình vẫn nằm trong lịch sử.</p>
        </template>
        <div class="space-y-4">
          <input ref="fileInput" type="file" accept="application/json,.json" class="hidden" @change="onFile">
          <UButton icon="i-lucide-upload" label="Chọn file config" color="neutral" variant="outline" @click="fileInput?.click()" />
          <template v-if="plan">
            <p class="text-sm">Xem trước <code>{{ fileName }}</code>: {{ pending }} thay đổi</p>
            <div class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
              <div v-for="(c, i) in plan" :key="i" class="flex flex-wrap items-center gap-3 px-3 py-2 text-sm">
                <UBadge :label="opMeta[c.op]!.label" :color="opMeta[c.op]!.color" variant="subtle" class="w-24 justify-center" />
                <span class="w-28 text-(--ui-text-muted)">{{ kindLabelOf[c.kind] }}</span>
                <span class="font-medium">{{ c.name }}</span>
                <span v-if="c.detail" class="text-xs text-(--ui-text-muted)">{{ c.detail }}</span>
              </div>
            </div>
            <div class="flex justify-end gap-2">
              <UButton color="neutral" variant="ghost" label="Hủy" @click="plan = null; bundle = null" />
              <UButton icon="i-lucide-check" :label="`Áp dụng ${pending} thay đổi`" :disabled="!pending" :loading="importing" @click="applyImport" />
            </div>
          </template>
        </div>
      </UCard>

      <UCard>
        <template #header>
          <p class="font-medium">Sao lưu dữ liệu</p>
          <p class="text-sm text-(--ui-text-muted)">
            Bản sao đầy đủ database và khóa mã hóa (gồm cả tài khoản và API key đã mã hóa), lưu trên máy chạy office. Không đưa bản này lên git.
          </p>
        </template>
        <div class="space-y-3">
          <UButton icon="i-lucide-archive" label="Tạo bản sao lưu" :loading="backingUp" @click="backup" />
          <UAlert v-if="backupPath" color="success" variant="subtle" icon="i-lucide-circle-check" title="Đã sao lưu">
            <template #description><code class="text-xs">{{ backupPath }}</code></template>
          </UAlert>
          <pre class="rounded-md bg-(--ui-bg-muted) p-3 text-xs">office backup --out ~/office-backups/$(date +%F)</pre>
        </div>
      </UCard>
    </div>
  </PageShell>
</template>
