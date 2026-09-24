<script setup lang="ts">
const toast = useToast()
const { isAdmin } = useAuth()
const { data, refresh } = await useFetch<{ templates: OrgModel[] }>('/api/templates')
const templates = computed(() => data.value?.templates ?? [])

const cloneOpen = ref(false)
const cloneSource = ref<OrgModel | null>(null)
const cloneForm = reactive({ key: '', name: '' })
const cloneError = ref('')

function openClone(t: OrgModel) {
  cloneSource.value = t
  cloneForm.key = `${t.key}-copy`
  cloneForm.name = `${t.name} (bản sao)`
  cloneError.value = ''
  cloneOpen.value = true
}

async function doClone() {
  try {
    const res = await $fetch<{ model: OrgModel }>('/api/templates', {
      method: 'POST', body: { source_id: cloneSource.value!.id, key: cloneForm.key, name: cloneForm.name }
    })
    cloneOpen.value = false
    await navigateTo(`/templates/${res.model.id}`)
  } catch (e) {
    const d = (e as { data?: { error?: string, problems?: string[] } }).data
    cloneError.value = d?.problems?.join('; ') ?? d?.error ?? 'Có lỗi xảy ra'
  }
}

async function reset(t: OrgModel) {
  if (!confirm(`Khôi phục "${t.name}" về mặc định? Mọi chỉnh sửa trên mẫu này sẽ mất (project đã áp không bị ảnh hưởng).`)) return
  await $fetch(`/api/templates/${t.key}/reset`, { method: 'POST' })
  await refresh()
  toast.add({ title: `Đã khôi phục ${t.name}`, color: 'success' })
}

async function remove(t: OrgModel) {
  if (!confirm(`Xóa mẫu "${t.name}"?`)) return
  await $fetch(`/api/org-models/${t.id}`, { method: 'DELETE' })
  await refresh()
}

function menu(t: OrgModel) {
  const items: { label: string, icon: string, color?: 'error', onSelect: () => void }[] = [
    { label: 'Nhân bản', icon: 'i-lucide-copy', onSelect: () => openClone(t) },
    { label: 'Tải JSON', icon: 'i-lucide-download', onSelect: () => window.open(`/api/org-models/${t.id}/export`, '_blank') }
  ]
  if (t.builtin) items.push({ label: 'Khôi phục mặc định', icon: 'i-lucide-rotate-ccw', onSelect: () => reset(t) })
  else items.push({ label: 'Xóa', icon: 'i-lucide-trash', color: 'error', onSelect: () => remove(t) })
  return [items]
}
</script>

<template>
  <PageShell title="Mô hình">
    <div class="space-y-4">
      <p class="max-w-3xl text-sm text-(--ui-text-muted)">
        Các mô hình để chọn khi thêm project hoặc chạy <b>init</b>. Mô hình của từng project được sửa ngay trong trang project;
        ở đây chỉ cần khi muốn chỉnh mô hình gốc hoặc lưu một mô hình để dùng lại.
      </p>
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <UCard v-for="t in templates" :key="t.id" class="flex flex-col">
          <div class="flex items-start justify-between gap-2">
            <NuxtLink :to="`/templates/${t.id}`" class="flex min-w-0 items-center gap-2 hover:text-primary">
              <UIcon :name="kindIcon[t.kind]" class="size-5 shrink-0 text-primary" />
              <span class="truncate font-semibold">{{ t.name }}</span>
            </NuxtLink>
            <div class="flex items-center gap-1">
              <UBadge v-if="t.builtin" label="Có sẵn" color="neutral" variant="outline" size="sm" />
              <UDropdownMenu v-if="isAdmin" :items="menu(t)">
                <UButton icon="i-lucide-ellipsis-vertical" color="neutral" variant="ghost" size="sm" />
              </UDropdownMenu>
            </div>
          </div>
          <p class="mt-2 line-clamp-3 text-sm text-(--ui-text-muted)">{{ t.description }}</p>
          <div class="mt-3 flex flex-wrap gap-1.5">
            <UBadge :label="kindLabel[t.kind]" variant="subtle" size="sm" />
            <UBadge v-for="(n, tier) in t.tiers" :key="tier" :label="`${n} ${tierLabel[tier as AgentTier]}`" color="neutral" variant="soft" size="sm" />
          </div>
          <div class="mt-4">
            <UButton :to="`/templates/${t.id}`" label="Xem và chỉnh sửa" variant="soft" size="sm" trailing-icon="i-lucide-arrow-right" />
          </div>
        </UCard>
      </div>
    </div>

    <UModal v-model:open="cloneOpen" :title="`Nhân bản ${cloneSource?.name ?? ''}`">
      <template #body>
        <form id="clone-form" class="space-y-4" @submit.prevent="doClone">
          <UFormField label="Tên" required>
            <UInput v-model="cloneForm.name" class="w-full" />
          </UFormField>
          <UFormField label="Key" required help="Dùng trong CLI: office init --template <key>">
            <UInput v-model="cloneForm.key" class="w-full font-mono" />
          </UFormField>
          <UAlert v-if="cloneError" color="error" variant="subtle" :description="cloneError" />
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="cloneOpen = false" />
          <UButton type="submit" form="clone-form" label="Nhân bản" />
        </div>
      </template>
    </UModal>
  </PageShell>
</template>
