<script setup lang="ts">
// A share of a limit (CPU, RAM, disk): the bar turns amber then red as it fills.
const props = defineProps<{ label: string, value: number, detail?: string }>()
const pct = computed(() => Math.max(0, Math.min(100, props.value)))
const fill = computed(() => pct.value >= 90 ? 'bg-(--ui-error)' : pct.value >= 70 ? 'bg-(--ui-warning)' : 'bg-(--ui-primary)')
</script>

<template>
  <div class="min-w-0">
    <div class="flex items-baseline justify-between gap-2 text-xs">
      <span class="truncate text-(--ui-text-muted)">{{ label }}</span>
      <span class="shrink-0 font-medium tabular-nums">{{ pct.toFixed(0) }}%</span>
    </div>
    <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-(--ui-primary)/15" role="meter" :aria-valuenow="pct" aria-valuemin="0" aria-valuemax="100" :aria-label="label">
      <div class="h-full rounded-full transition-all duration-500" :class="fill" :style="{ width: `${pct}%` }" />
    </div>
    <p v-if="detail" class="mt-0.5 truncate text-[11px] text-(--ui-text-dimmed) tabular-nums">{{ detail }}</p>
  </div>
</template>
