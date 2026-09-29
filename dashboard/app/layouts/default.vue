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

// pages below a project (/projects/:id/<child>/…) belong to one of its sections
const childSection: Record<string, string> = { agents: 'model', automations: 'automations' }

function projectSections(id: string): NavigationMenuItem[] {
  const to = (tab: string) => ({ path: `/projects/${id}`, query: { tab } })
  const child = route.path.match(new RegExp(`^/projects/${id}/([^/]+)/`))?.[1]
  const parent = child ? childSection[child] : undefined
  return withParent(parent, [
    { label: t('nav.chat'), icon: 'i-lucide-messages-square', to: to('chat'), exactQuery: 'partial' },
    { label: t('nav.tasks'), icon: 'i-lucide-list-todo', to: to('tasks'), exactQuery: 'partial' },
    { label: t('nav.automations'), icon: 'i-lucide-alarm-clock', to: to('automations'), exactQuery: 'partial' },
    { label: t('nav.ops'), icon: 'i-lucide-activity', to: to('ops'), exactQuery: 'partial' },
    { label: t('project.sectionModel'), icon: 'i-lucide-users', to: to('model'), exactQuery: 'partial' },
    { label: t('project.sectionPerm'), icon: 'i-lucide-shield', to: to('perm'), exactQuery: 'partial' },
    ...(isAdmin.value
      ? [
          { label: t('project.sectionSkill'), icon: 'i-lucide-sparkles', to: to('skill'), exactQuery: 'partial' as const },
          { label: t('project.sectionMcp'), icon: 'i-lucide-plug-zap', to: to('mcp'), exactQuery: 'partial' as const },
          { label: t('nav.log'), icon: 'i-lucide-scroll-text', to: to('log'), exactQuery: 'partial' as const }
        ]
      : []),
    { label: t('project.sectionInfo'), icon: 'i-lucide-info', to: to('info'), exactQuery: 'partial' }
  ])
}

// marks the section a child page belongs to as the active one
function withParent(parent: string | undefined, items: NavigationMenuItem[]): NavigationMenuItem[] {
  if (!parent) return items
  return items.map(x => (x.to as { query?: { tab?: string } })?.query?.tab === parent ? { ...x, active: true } : x)
}

// what needs a person across the office: its count on "Sự cố"
const { data: incData, refresh: refreshInc } = useFetch<{ count: number }>('/api/incidents', { lazy: true })
const attention = computed(() => incData.value?.count ?? 0)
let incTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => { incTimer = setInterval(() => refreshInc(), 60000) })
onBeforeUnmount(() => clearInterval(incTimer))

const items = computed<NavigationMenuItem[][]>(() => {
  // office-wide pages: the day's work, then how office is set up
  const office: NavigationMenuItem[] = [
    { label: t('nav.work'), type: 'label' },
    // one place to follow the office: what needs a person is counted here
    { label: t('nav.overview'), icon: 'i-lucide-layout-dashboard', to: '/', badge: attention.value || undefined },
    { label: t('assistant.title'), icon: 'i-lucide-sparkles', to: '/assistant' },
    { label: t('nav.jobs'), icon: 'i-lucide-list-checks', to: '/jobs' }
  ]
  const settings: NavigationMenuItem[] = [
    { label: t('nav.settings'), type: 'label' },
    { label: t('nav.providers'), icon: 'i-lucide-plug', to: '/providers' },
    { label: t('nav.templates'), icon: 'i-lucide-network', to: '/templates' },
    ...(isAdmin.value ? [{ label: t('nav.library'), icon: 'i-lucide-library', to: '/library' }] : []),
    { label: t('nav.costs'), icon: 'i-lucide-wallet', to: '/costs' }
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
  // projects: a title, the most used ones, then a link to all of them
  const projects: NavigationMenuItem[] = [
    { label: t('nav.projects'), type: 'label' },
    ...recentProjects.value.map(p => ({
      value: `project-${p.id}`, // open state follows the project, not its position
      label: p.name,
      icon: p.scope === 'machine' ? 'i-lucide-monitor' : 'i-lucide-folder',
      to: { path: `/projects/${p.id}`, query: { tab: 'chat' } },
      defaultOpen: route.params.id === p.id,
      children: projectSections(p.id)
    })),
    projectList.value.length
      ? { label: t('nav.allProjects'), icon: 'i-lucide-list', to: '/projects', exact: true }
      : { label: t('nav.addProject'), icon: 'i-lucide-plus', to: '/projects', exact: true }
  ]
  return [projects, office, settings, admin]
})

const userMenu = computed<DropdownMenuItem[][]>(() => [
  [{ label: user.value?.email ?? '', type: 'label' }],
  [{ label: t('user.changePassword'), icon: 'i-lucide-key-round', to: '/account' }, { label: t('cli.title'), icon: 'i-lucide-terminal', to: '/account' }],
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
        <div class="flex w-full min-w-0 items-center gap-2 px-1 font-semibold">
          <UIcon name="i-lucide-building-2" class="size-5 shrink-0 text-primary" />
          <span v-if="!collapsed" class="min-w-0 truncate">Agent Office</span>
          <PrefSwitcher v-if="!collapsed" class="ms-auto shrink-0" />
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
    <FloatingChat />
  </UDashboardGroup>
</template>
