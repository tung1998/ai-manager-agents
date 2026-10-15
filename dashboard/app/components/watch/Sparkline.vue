<script setup lang="ts">
// A small trend line (area under it), last point marked; values only, no axes.
// bad: points drawn as failures (a monitor's failed checks), in red.
// fill: as tall as its container (a big box), not one line
const props = defineProps<{ values: number[], bad?: boolean[], label?: string, fill?: boolean }>()
const W = 100
const H = 28
const pts = computed(() => {
  const v = props.values
  if (v.length < 2) return []
  const max = Math.max(...v)
  const min = Math.min(...v)
  const span = max - min || 1
  return v.map((x, i) => ({ x: i / (v.length - 1) * W, y: H - 2 - (x - min) / span * (H - 4) }))
})
const line = computed(() => pts.value.map((p, i) => `${i ? 'L' : 'M'}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(''))
const area = computed(() => pts.value.length ? `${line.value}L${W},${H}L0,${H}Z` : '')
const last = computed(() => pts.value.at(-1))
</script>

<template>
  <svg v-if="pts.length" :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" :class="fill ? 'h-full min-h-7' : 'h-7'" class="w-full overflow-visible text-(--ui-primary)" role="img" :aria-label="label">
    <path :d="area" fill="currentColor" opacity="0.12" />
    <path :d="line" fill="none" stroke="currentColor" stroke-width="1.5" vector-effect="non-scaling-stroke" stroke-linejoin="round" />
    <template v-for="(p, i) in pts" :key="i">
      <!-- dots as zero-length round lines: they stay round when the svg stretches -->
      <line v-if="bad?.[i]" :x1="p.x" :y1="p.y" :x2="p.x" :y2="p.y" stroke-width="4" stroke-linecap="round" vector-effect="non-scaling-stroke" class="stroke-(--ui-error)" />
    </template>
    <line v-if="last" :x1="last.x" :y1="last.y" :x2="last.x" :y2="last.y" stroke="currentColor" stroke-width="5" stroke-linecap="round" vector-effect="non-scaling-stroke" />
  </svg>
</template>
