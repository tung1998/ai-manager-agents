<script setup lang="ts">
import type { DropdownMenuItem, NavigationMenuItem } from '@nuxt/ui'

const route = useRoute()
const { user, isAdmin, logout } = useAuth()
const { t } = useLang()

const bareLayout = computed(() => route.path === '/login')

// sidebar: the 5 projects this viewer opens most, each with its sections
// (refetched on navigation so added/renamed projects show up)
const projectList = ref<Project[]>([])
const { top } = useProjectUsage()
const recentProjects = computed(() => top(projectList.value, 5, route.params.id as string | undefined))
watch(() => route.path, async () => {
  if (bareLayout.value) return
  try {
    projectList.value = (await $fetch<{ projects: Project[] }>('/api/projects')).projects
  } catch { /* signed out: the auth middleware redirects */ }
}, { immediate: true })

function projectSections(id: string): NavigationMenuItem[] {
  const to = (tab: string) => ({ path: `/projects/${id}`, query: { tab } })
  return [
    { label: t('nav.chat'), icon: 'i-lucide-messages-square', to: to('chat'), exactQuery: 'partial' },
    { label: t('nav.tasks'), icon: 'i-lucide-list-todo', to: to('tasks'), exactQuery: 'partial' },
    { label: t('nav.ops'), icon: 'i-lucide-activity', to: to('ops'), exactQuery: 'partial' },
    { label: t('nav.config'), icon: 'i-lucide-settings-2', to: to('config'), exactQuery: 'partial' }
  ]
}

const items = computed<NavigationMenuItem[][]>(() => {
  const main: NavigationMenuItem[] = [
    { label: t('nav.overview'), icon: 'i-lucide-layout-dashboard', to: '/' },
    {
      // an item with both a link and children navigates on click; only the chevron toggles
      label: t('nav.projects'),
      icon: 'i-lucide-folder-git-2',
      to: '/projects',
      exact: true,
      defaultOpen: true,
      children: [
        ...recentProjects.value.map(p => ({
          value: `project-${p.id}`, // open state follows the project, not its position
          label: p.name,
          icon: p.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder',
          to: `/projects/${p.id}`,
          defaultOpen: route.params.id === p.id,
          children: projectSections(p.id)
        })),
        // "…" only when some projects are hidden
        ...(projectList.value.length > 5 ? [{ icon: 'i-lucide-ellipsis', to: '/projects', exact: true, 'aria-label': t('nav.allProjects') }] : [])
      ]
    },
    { label: t('nav.providers'), icon: 'i-lucide-plug', to: '/providers' },
    { label: t('nav.blackboard'), icon: 'i-lucide-messages-square', to: '/blackboard', badge: 'M4' },
    { label: t('nav.incidents'), icon: 'i-lucide-siren', to: '/incidents', badge: 'M4' },
    { label: t('nav.costs'), icon: 'i-lucide-wallet', to: '/costs' },
    { label: t('nav.templates'), icon: 'i-lucide-network', to: '/templates' },
    ...(isAdmin.value ? [{ label: t('nav.library'), icon: 'i-lucide-library', to: '/library' }] : [])
  ]
  const admin: NavigationMenuItem[] = isAdmin.value
    ? [
        { label: t('nav.admin'), type: 'label' },
        { label: t('nav.users'), icon: 'i-lucide-users', to: '/admin/users' },
        { label: t('nav.audit'), icon: 'i-lucide-scroll-text', to: '/admin/audit' },
        { label: t('nav.transfer'), icon: 'i-lucide-archive-restore', to: '/admin/transfer' },
        { label: t('nav.update'), icon: 'i-lucide-package', to: '/admin/update' }
      ]
    : []
  return [main, admin]
})

const userMenu = computed<DropdownMenuItem[][]>(() => [
  [{ label: user.value?.email ?? '', type: 'label' }],
  [{ label: t('user.changePassword'), icon: 'i-lucide-key-round', to: '/account' }],
  [{ label: t('user.logout'), icon: 'i-lucide-log-out', color: 'error', onSelect: () => logout() }]
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
          <PrefSwitcher v-if="!collapsed" class="ms-auto" />
        </div>
      </template>

      <template #default="{ collapsed }">
        <!-- remount when the open project or the list changes so its sections expand -->
        <UNavigationMenu :key="`${route.params.id ?? ''}:${projectList.length}`" :collapsed="collapsed" :items="items" orientation="vertical" />
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
