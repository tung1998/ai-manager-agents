<script setup lang="ts">
// One figure with its name (CPU 3%, RAM 120 MB, uptime 2h): the small facts of a box.
defineProps<{ label: string, value: string, icon?: string }>()
</script>

<template>
  <div class="min-w-0 rounded-md bg-(--ui-bg-elevated)/60 px-2 py-1.5">
    <p class="flex items-center gap-1 truncate text-[11px] text-(--ui-text-muted)">
      <UIcon v-if="icon" :name="icon" class="size-3 shrink-0" />{{ label }}
    </p>
    <p class="truncate text-sm font-semibold">{{ value }}</p>
  </div>
</template>
