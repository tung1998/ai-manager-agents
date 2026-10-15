<script setup lang="ts">
// One AI connection's mark: its logo in its usage rings (useProviderRings).
// Fills its parent; the logo scales with it.
import type { ProviderRingData } from '~/composables/useProviderRings'

defineProps<{ ring: ProviderRingData, logoClass?: string }>()
const W = 1.5 // every ring the same width
</script>

<template>
  <span class="relative grid place-items-center">
    <svg viewBox="0 0 24 24" class="absolute inset-0 size-full -rotate-90">
      <template v-for="a in ring.arcs" :key="a.r">
        <circle cx="12" cy="12" :r="a.r" fill="none" :stroke-width="W" class="stroke-(--ui-bg-accented)" />
        <circle
          cx="12" cy="12" :r="a.r" fill="none" :stroke-width="W" stroke-linecap="round" :class="a.stroke"
          :stroke-dasharray="`${a.share * a.c} ${a.c}`" class="transition-all"
        />
      </template>
    </svg>
    <UIcon v-if="ring.logo" :name="ring.logo" :class="[logoClass ?? 'size-3', ring.logoColor]" />
    <span v-else class="grid place-items-center rounded-full font-semibold text-white" :class="[logoClass ?? 'size-3.5 text-[8px]', ring.bg]">{{ ring.letter }}</span>
  </span>
</template>
