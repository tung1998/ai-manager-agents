<script setup lang="ts">
// A box with nothing to show yet: what it needs (pick a process…), the control
// right under it. In a small box: one line, the text clamped (its full text on hover).
defineProps<{ icon: string, text: string }>()
const box = inject(watchBoxKey, null)
const small = computed(() => !!box && (box.size.value.h < 150 || box.size.value.w < 200))
</script>

<template>
  <div v-if="small" class="flex h-full min-h-0 items-center gap-2 overflow-hidden p-2" :title="text">
    <UIcon :name="icon" class="size-4 shrink-0 text-(--ui-text-dimmed)" />
    <p class="line-clamp-2 min-w-0 text-xs text-(--ui-text-muted)">{{ text }}</p>
  </div>
  <div v-else class="flex h-full min-h-0 flex-col items-center justify-center gap-3 overflow-hidden p-4 text-center">
    <UIcon :name="icon" class="size-8 shrink-0 text-(--ui-text-dimmed)" />
    <p class="line-clamp-4 text-sm text-(--ui-text-muted)" :title="text">{{ text }}</p>
    <div class="w-full max-w-60"><slot /></div>
  </div>
</template>
