<script setup lang="ts">
// A state as a dot and a word (never color alone): ok, warn, bad, idle, busy.
// lg: the box's headline when it has room (a process, a monitor).
export type WatchTone = 'ok' | 'warn' | 'bad' | 'idle' | 'busy'
const props = defineProps<{ tone: WatchTone, label: string, size?: 'sm' | 'lg' }>()
const meta = computed(() => ({
  ok: { dot: 'bg-(--ui-success)', text: 'text-(--ui-success)', icon: 'i-lucide-circle-check' },
  warn: { dot: 'bg-(--ui-warning)', text: 'text-(--ui-warning)', icon: 'i-lucide-triangle-alert' },
  bad: { dot: 'bg-(--ui-error)', text: 'text-(--ui-error)', icon: 'i-lucide-circle-x' },
  busy: { dot: 'bg-(--ui-info)', text: 'text-(--ui-info)', icon: 'i-lucide-loader-circle' },
  idle: { dot: 'bg-(--ui-text-dimmed)', text: 'text-(--ui-text-muted)', icon: 'i-lucide-circle-pause' }
})[props.tone])
</script>

<template>
  <span v-if="size === 'lg'" class="inline-flex min-w-0 items-center gap-2 font-semibold" :class="meta.text">
    <UIcon :name="meta.icon" class="size-6 shrink-0" :class="{ 'animate-spin': tone === 'busy' }" />
    <span class="truncate text-xl">{{ label }}</span>
  </span>
  <span v-else class="inline-flex min-w-0 items-center gap-1.5 text-xs font-medium" :class="meta.text">
    <span class="relative flex size-2 shrink-0">
      <span v-if="tone === 'ok' || tone === 'busy'" class="absolute inline-flex size-full animate-ping rounded-full opacity-50" :class="meta.dot" />
      <span class="relative inline-flex size-2 rounded-full" :class="meta.dot" />
    </span>
    <span class="truncate">{{ label }}</span>
  </span>
</template>
