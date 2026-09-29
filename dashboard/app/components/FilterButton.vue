<script setup lang="ts">
// One filter icon (with how many filters are on); the filters open under it.
defineProps<{ active?: number }>()
const { t } = useLang()
</script>

<template>
  <UPopover :content="{ align: 'end' }">
    <UButton
      size="sm" color="neutral" :variant="active ? 'soft' : 'ghost'" icon="i-lucide-list-filter" :aria-label="t('filter.title')"
      :label="active ? String(active) : undefined" class="shrink-0"
    />
    <template #content>
      <div class="w-64 space-y-2 p-3">
        <p class="text-xs font-medium text-(--ui-text-muted)">{{ t('filter.title') }}</p>
        <slot />
      </div>
    </template>
  </UPopover>
</template>
