<script setup lang="ts">
import type { DropdownMenuItem, NavigationMenuItem } from '@nuxt/ui'

const route = useRoute()
const { user, isAdmin, logout } = useAuth()

const bareLayout = computed(() => route.path === '/login')

const items = computed<NavigationMenuItem[][]>(() => {
  const main: NavigationMenuItem[] = [
    { label: 'Tổng quan', icon: 'i-lucide-layout-dashboard', to: '/' },
    { label: 'Project', icon: 'i-lucide-folder-git-2', to: '/projects' },
    { label: 'Mô hình mẫu', icon: 'i-lucide-network', to: '/templates' },
    { label: 'Kết nối AI', icon: 'i-lucide-plug', to: '/providers' },
    { label: 'Blackboard', icon: 'i-lucide-messages-square', to: '/blackboard', badge: 'M4' },
    { label: 'Incidents', icon: 'i-lucide-siren', to: '/incidents', badge: 'M4' },
    { label: 'Chi phí', icon: 'i-lucide-wallet', to: '/costs', badge: 'M4' }
  ]
  const admin: NavigationMenuItem[] = isAdmin.value
    ? [
        { label: 'Quản trị', type: 'label' },
        { label: 'Tài khoản', icon: 'i-lucide-users', to: '/admin/users' },
        { label: 'Audit log', icon: 'i-lucide-scroll-text', to: '/admin/audit' },
        { label: 'Sao lưu & đồng bộ', icon: 'i-lucide-archive-restore', to: '/admin/transfer' }
      ]
    : []
  return [main, admin]
})

const userMenu = computed<DropdownMenuItem[][]>(() => [
  [{ label: user.value?.email ?? '', type: 'label' }],
  [{ label: 'Đổi mật khẩu', icon: 'i-lucide-key-round', to: '/account' }],
  [{ label: 'Đăng xuất', icon: 'i-lucide-log-out', color: 'error', onSelect: () => logout() }]
])
</script>

<template>
  <div v-if="bareLayout" class="min-h-screen">
    <slot />
  </div>

  <UDashboardGroup v-else>
    <UDashboardSidebar collapsible resizable :default-size="18">
      <template #header="{ collapsed }">
        <div class="flex items-center gap-2 px-1 font-semibold">
          <UIcon name="i-lucide-building-2" class="size-5 text-primary" />
          <span v-if="!collapsed">agent-office</span>
        </div>
      </template>

      <template #default="{ collapsed }">
        <UNavigationMenu :collapsed="collapsed" :items="items" orientation="vertical" />
      </template>

      <template #footer="{ collapsed }">
        <UDropdownMenu :items="userMenu" :content="{ align: 'start' }" class="w-full">
          <UButton
            color="neutral"
            variant="ghost"
            block
            :square="collapsed"
            class="justify-start"
            :avatar="{ alt: user?.name || user?.email }"
            :label="collapsed ? undefined : (user?.name || user?.email)"
            trailing-icon="i-lucide-chevrons-up-down"
            :ui="{ trailingIcon: collapsed ? 'hidden' : 'ms-auto' }"
          />
        </UDropdownMenu>
      </template>
    </UDashboardSidebar>

    <slot />
  </UDashboardGroup>
</template>
