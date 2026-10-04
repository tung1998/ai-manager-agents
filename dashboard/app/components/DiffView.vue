<script setup lang="ts">
// a unified diff, coloured line by line (patch cards, the file editor)
const props = defineProps<{ diff: string }>()

const lines = computed(() => props.diff.split('\n').map((l) => {
  const kind = l.startsWith('+++') || l.startsWith('---') ? 'file'
    : l.startsWith('@@') ? 'hunk'
      : l.startsWith('+') ? 'add'
        : l.startsWith('-') ? 'del' : 'ctx'
  return { text: l, kind }
}))
</script>

<template>
  <pre class="overflow-auto py-1 text-xs leading-5"><code><span
    v-for="(l, i) in lines" :key="i" class="block px-3"
    :class="{
      'bg-(--ui-success)/10 text-(--ui-success)': l.kind === 'add',
      'bg-(--ui-error)/10 text-(--ui-error)': l.kind === 'del',
      'text-(--ui-text-muted)': l.kind === 'hunk' || l.kind === 'file'
    }"
  >{{ l.text || ' ' }}</span></code></pre>
</template>
