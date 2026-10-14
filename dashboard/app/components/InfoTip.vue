<script setup lang="ts">
// ⓘ with its explanation in a tooltip: hover on desktop, tap on touch screens
// (a plain tooltip ignores touch). The click never reaches a surrounding label/switch.
defineProps<{ text?: string, iconClass?: string }>()
const open = ref(false)
</script>

<template>
  <UTooltip v-if="text" v-model:open="open" :text="text" disable-closing-trigger>
    <UIcon
      name="i-lucide-info" class="shrink-0 cursor-help hover:text-(--ui-text)"
      :class="iconClass ?? 'size-3.5 text-(--ui-text-dimmed)'"
      @click.prevent.stop="open = !open"
    />
  </UTooltip>
</template>
