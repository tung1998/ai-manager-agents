<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'

definePageMeta({ admin: true })

const toast = useToast()
const { user: me } = useAuth()
const { data, refresh, status } = await useFetch<{ users: OfficeUser[] }>('/api/users')
const users = computed(() => data.value?.users ?? [])

const columns: TableColumn<OfficeUser>[] = [
  { accessorKey: 'email', header: 'Email' },
  { accessorKey: 'name', header: 'Tên' },
  { accessorKey: 'role', header: 'Role' },
  { accessorKey: 'disabled', header: 'Trạng thái' },
  { accessorKey: 'last_login_at', header: 'Đăng nhập gần nhất' },
  { id: 'actions' }
]

function fmt(d: string | null) {
  return d ? new Date(d).toLocaleString('vi-VN') : '—'
}

// ---- create ----
const createOpen = ref(false)
const createState = reactive({ email: '', name: '', role: 'member', password: '' })
const createError = ref('')
const creating = ref(false)

async function createUser() {
  createError.value = ''
  creating.value = true
  try {
    await $fetch('/api/users', { method: 'POST', body: { ...createState } })
    toast.add({ title: `Đã tạo ${createState.email}`, color: 'success' })
    Object.assign(createState, { email: '', name: '', role: 'member', password: '' })
    createOpen.value = false
    await refresh()
  } catch (e) {
    createError.value = apiError(e)
  } finally {
    creating.value = false
  }
}

// ---- disable / enable ----
async function setDisabled(u: OfficeUser, disabled: boolean) {
  try {
    await $fetch(`/api/users/${u.id}`, { method: 'PATCH', body: { disabled } })
    toast.add({ title: disabled ? `Đã vô hiệu hóa ${u.email}` : `Đã mở lại ${u.email}`, color: 'success' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

// ---- reset password ----
const resetTarget = ref<OfficeUser | null>(null)
const resetPassword = ref('')
const resetError = ref('')

async function doReset() {
  if (!resetTarget.value) return
  resetError.value = ''
  try {
    await $fetch(`/api/users/${resetTarget.value.id}/reset-password`, { method: 'POST', body: { password: resetPassword.value } })
    toast.add({ title: `Đã đặt lại mật khẩu cho ${resetTarget.value.email}`, color: 'success' })
    resetTarget.value = null
    resetPassword.value = ''
  } catch (e) {
    resetError.value = apiError(e)
  }
}

function rowActions(u: OfficeUser) {
  const items = [[{ label: 'Đặt lại mật khẩu', icon: 'i-lucide-key-round', onSelect: () => { resetTarget.value = u } }]]
  if (u.id !== me.value?.id) {
    items.push([u.disabled
      ? { label: 'Mở lại', icon: 'i-lucide-user-check', onSelect: () => setDisabled(u, false) }
      : { label: 'Vô hiệu hóa', icon: 'i-lucide-user-x', color: 'error', onSelect: () => setDisabled(u, true) }] as never)
  }
  return items
}
</script>

<template>
  <PageShell title="Tài khoản">
    <template #actions>
      <UButton icon="i-lucide-user-plus" label="Thêm tài khoản" @click="createOpen = true" />
    </template>

    <UTable :data="users" :columns="columns" :loading="status === 'pending'">
      <template #role-cell="{ row }">
        <UBadge :label="row.original.role" :color="row.original.role === 'admin' ? 'primary' : 'neutral'" variant="subtle" />
      </template>
      <template #disabled-cell="{ row }">
        <UBadge
          :label="row.original.disabled ? 'Vô hiệu' : 'Hoạt động'"
          :color="row.original.disabled ? 'error' : 'success'"
          variant="subtle"
        />
      </template>
      <template #last_login_at-cell="{ row }">
        {{ fmt(row.original.last_login_at) }}
      </template>
      <template #actions-cell="{ row }">
        <div class="text-right">
          <UDropdownMenu :items="rowActions(row.original)">
            <UButton icon="i-lucide-ellipsis-vertical" color="neutral" variant="ghost" />
          </UDropdownMenu>
        </div>
      </template>
    </UTable>

    <UModal v-model:open="createOpen" title="Thêm tài khoản">
      <template #body>
        <form id="create-user" class="space-y-4" @submit.prevent="createUser">
          <UFormField label="Email" required>
            <UInput v-model="createState.email" type="email" class="w-full" />
          </UFormField>
          <UFormField label="Tên hiển thị">
            <UInput v-model="createState.name" class="w-full" />
          </UFormField>
          <UFormField label="Role">
            <USelect v-model="createState.role" :items="[{ label: 'Member (xem, hỏi)', value: 'member' }, { label: 'Admin (quản trị)', value: 'admin' }]" class="w-full" />
          </UFormField>
          <UFormField label="Mật khẩu tạm" hint="Tối thiểu 10 ký tự" required>
            <UInput v-model="createState.password" type="password" autocomplete="new-password" class="w-full" />
          </UFormField>
          <UAlert v-if="createError" color="error" variant="subtle" :description="createError" />
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="createOpen = false" />
          <UButton type="submit" form="create-user" :loading="creating" label="Tạo" />
        </div>
      </template>
    </UModal>

    <UModal :open="!!resetTarget" :title="`Đặt lại mật khẩu: ${resetTarget?.email ?? ''}`" @update:open="v => { if (!v) resetTarget = null }">
      <template #body>
        <form id="reset-pw" class="space-y-4" @submit.prevent="doReset">
          <p class="text-sm text-(--ui-text-muted)">Mọi phiên đăng nhập của tài khoản này sẽ bị đăng xuất.</p>
          <UFormField label="Mật khẩu mới" hint="Tối thiểu 10 ký tự" required>
            <UInput v-model="resetPassword" type="password" autocomplete="new-password" class="w-full" />
          </UFormField>
          <UAlert v-if="resetError" color="error" variant="subtle" :description="resetError" />
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="resetTarget = null" />
          <UButton type="submit" form="reset-pw" label="Đặt lại" />
        </div>
      </template>
    </UModal>
  </PageShell>
</template>
