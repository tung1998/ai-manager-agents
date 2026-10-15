<script setup lang="ts">
// A share of a limit as a ring (a budget spent): amber from 70%, red from 90%;
// the figure inside, the label under it.
const props = defineProps<{ value: number, label?: string, size?: number }>()
const pct = computed(() => Math.max(0, Math.min(100, props.value)))
const R = 42
const C = 2 * Math.PI * R
const stroke = computed(() => pct.value >= 90 ? 'stroke-(--ui-error)' : pct.value >= 70 ? 'stroke-(--ui-warning)' : 'stroke-(--ui-primary)')
</script>

<template>
  <div class="relative inline-flex shrink-0 items-center justify-center" :style="{ width: `${size ?? 96}px`, height: `${size ?? 96}px` }">
    <svg viewBox="0 0 100 100" class="absolute inset-0 -rotate-90" role="meter" :aria-valuenow="pct" aria-valuemin="0" aria-valuemax="100" :aria-label="label">
      <circle cx="50" cy="50" :r="R" fill="none" stroke-width="9" class="stroke-(--ui-primary)/15" />
      <circle cx="50" cy="50" :r="R" fill="none" stroke-width="9" stroke-linecap="round" :class="stroke" :stroke-dasharray="`${pct / 100 * C} ${C}`" class="transition-all duration-500" />
    </svg>
    <span class="relative text-center leading-tight"><slot /></span>
  </div>
</template>
