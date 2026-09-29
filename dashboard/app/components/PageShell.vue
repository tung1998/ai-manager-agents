<script setup lang="ts">
defineProps<{ title: string }>()
</script>

<template>
  <!-- the page never scrolls sideways: wide tables scroll inside their own box -->
  <UDashboardPanel :ui="{ body: 'overflow-x-hidden min-w-0' }">
    <template #header>
      <UDashboardNavbar :title="title">
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
          <slot name="actions" />
        </template>
      </UDashboardNavbar>
    </template>
    <template #body>
      <slot />
    </template>
  </UDashboardPanel>
</template>
