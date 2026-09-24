<script setup lang="ts">
const toast = useToast()
const state = reactive({ old_password: '', new_password: '', confirm: '' })
const loading = ref(false)
const error = ref('')

async function onSubmit() {
  error.value = ''
  if (state.new_password !== state.confirm) {
    error.value = 'Hai lần nhập mật khẩu mới không khớp'
    return
  }
  loading.value = true
  try {
    await $fetch('/api/auth/password', {
      method: 'POST',
      body: { old_password: state.old_password, new_password: state.new_password }
    })
    Object.assign(state, { old_password: '', new_password: '', confirm: '' })
    toast.add({ title: 'Đã đổi mật khẩu', description: 'Các phiên đăng nhập khác đã bị đăng xuất.', color: 'success' })
  } catch (e) {
    error.value = apiError(e)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <PageShell title="Đổi mật khẩu">
    <UCard class="max-w-md">
      <form class="space-y-4" @submit.prevent="onSubmit">
        <UFormField label="Mật khẩu hiện tại" required>
          <UInput v-model="state.old_password" type="password" autocomplete="current-password" class="w-full" />
        </UFormField>
        <UFormField label="Mật khẩu mới" hint="Tối thiểu 10 ký tự" required>
          <UInput v-model="state.new_password" type="password" autocomplete="new-password" class="w-full" />
        </UFormField>
        <UFormField label="Nhập lại mật khẩu mới" required>
          <UInput v-model="state.confirm" type="password" autocomplete="new-password" class="w-full" />
        </UFormField>
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <UButton type="submit" :loading="loading" label="Lưu" />
      </form>
    </UCard>
  </PageShell>
</template>
