<script setup lang="ts">
// Columns over days, one or two parts each (done, failed), a tooltip per
// column; the axis shows the first and last day only.
export interface WatchBar { key: string, label: string, parts: number[], tip: string }
const props = defineProps<{ bars: WatchBar[], colors: string[], legend?: string[] }>()
const max = computed(() => Math.max(1, ...props.bars.map(b => b.parts.reduce((a, c) => a + c, 0))))
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-1">
    <div v-if="legend?.length && legend.length > 1" class="flex flex-wrap gap-3 text-[11px] text-(--ui-text-muted)">
      <span v-for="(l, i) in legend" :key="l" class="inline-flex items-center gap-1"><span class="size-2 rounded-sm" :class="colors[i]" />{{ l }}</span>
    </div>
    <div class="flex min-h-0 flex-1 items-end gap-0.5 border-b border-(--ui-border)">
      <UTooltip v-for="b in bars" :key="b.key" :text="b.tip" :content="{ side: 'top' }">
        <div class="flex h-full min-w-0 flex-1 flex-col justify-end hover:bg-(--ui-bg-elevated)/60">
          <div class="flex flex-col-reverse gap-px overflow-hidden rounded-t-[3px]" :style="{ height: `${b.parts.reduce((a, c) => a + c, 0) / max * 100}%` }">
            <div v-for="(v, i) in b.parts" :key="i" class="w-full" :class="colors[i]" :style="{ flex: `${v} 0 0` }" />
          </div>
        </div>
      </UTooltip>
    </div>
    <div class="flex justify-between text-[11px] text-(--ui-text-dimmed) tabular-nums">
      <span>{{ bars[0]?.label }}</span><span>{{ bars.at(-1)?.label }}</span>
    </div>
  </div>
</template>
