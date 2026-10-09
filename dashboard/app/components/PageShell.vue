<script setup lang="ts">
defineProps<{ title: string }>()
const { isAdmin } = useAuth()
// the FAB (FloatingChat) sits fixed over the bottom-right corner on mobile;
// reserve room so it never covers the page's last row/control
const fabVisible = useFabVisible()
</script>

<template>
  <!-- the page never scrolls sideways: wide tables scroll inside their own box;
       each block takes the full width (a centered max-w one never grows to its content) -->
  <UDashboardPanel :ui="{ body: `overflow-x-hidden min-w-0 max-sm:p-3 *:w-full ${fabVisible ? 'max-sm:pb-20' : ''}` }">
    <template #header>
      <UDashboardNavbar :title="title" :ui="{ root: 'max-sm:px-3' }">
        <template #leading>
          <UDashboardSidebarCollapse />
        </template>
        <template v-if="$slots.subtitle" #title>
          <!-- a second line under the title (e.g. the project's git state) -->
          <div class="min-w-0 leading-tight">
            <span class="block truncate">{{ title }}</span>
            <div class="truncate text-xs font-normal text-(--ui-text-muted)">
              <slot name="subtitle" />
            </div>
          </div>
        </template>
        <template #right>
          <HeaderStats v-if="isAdmin" />
          <div class="flex items-center gap-1.5 max-sm:[&_[data-slot=base]:has([data-slot=leadingIcon])_[data-slot=label]]:sr-only">
            <slot name="actions" />
          </div>
        </template>
      </UDashboardNavbar>
    </template>
    <template #body>
      <slot />
    </template>
  </UDashboardPanel>
</template>
