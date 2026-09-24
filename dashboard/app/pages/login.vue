<script setup lang="ts">
const route = useRoute()
const { login } = useAuth()

const state = reactive({ email: '', password: '' })
const loading = ref(false)
const error = ref('')

const { data: status } = await useFetch<{ has_users: boolean }>('/api/auth/status')

async function onSubmit() {
  error.value = ''
  loading.value = true
  try {
    await login(state.email, state.password)
    const r = route.query.redirect
    await navigateTo(typeof r === 'string' && r.startsWith('/') && !r.startsWith('//') ? r : '/')
  } catch (e) {
    error.value = apiError(e, 'Không đăng nhập được')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-(--ui-bg-muted) px-4">
    <UCard class="w-full max-w-sm">
      <template #header>
        <div class="flex items-center gap-2">
          <UIcon name="i-lucide-building-2" class="size-6 text-primary" />
          <div>
            <h1 class="text-lg font-semibold">agent-office</h1>
            <p class="text-sm text-(--ui-text-muted)">Đăng nhập trang quản trị</p>
          </div>
        </div>
      </template>

      <UAlert
        v-if="status && !status.has_users"
        class="mb-4"
        color="warning"
        variant="subtle"
        icon="i-lucide-info"
        title="Chưa có tài khoản"
        description="Chạy lệnh sau trên máy chủ để tạo admin đầu tiên: office user create --email you@company.com --role admin"
      />

      <form class="space-y-4" @submit.prevent="onSubmit">
        <UFormField label="Email" name="email" required>
          <UInput v-model="state.email" type="email" autocomplete="username" placeholder="you@company.com" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Mật khẩu" name="password" required>
          <UInput v-model="state.password" type="password" autocomplete="current-password" class="w-full" />
        </UFormField>

        <UAlert v-if="error" color="error" variant="subtle" icon="i-lucide-circle-alert" :description="error" />

        <UButton type="submit" block :loading="loading" label="Đăng nhập" />
      </form>
    </UCard>
  </div>
</template>
