<script setup lang="ts">
// A field label with its explanation behind an ⓘ instead of a paragraph under the field.
defineProps<{ label?: string, info?: string }>()
</script>

<template>
  <span class="inline-flex items-center gap-1.5">
    {{ label }}
    <InfoTip :text="info" />
  </span>
</template>
