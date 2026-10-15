<script setup lang="ts">
// A note on the board (what to watch for, a command to remember): written
// right in the box, kept with the board.
const { t } = useLang()
const { config, set, tiny } = useWatchBox()
const text = ref(String(config.value.text ?? ''))
let timer: ReturnType<typeof setTimeout> | undefined
watch(text, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => set('text', v), 400)
})
onBeforeUnmount(() => {
  clearTimeout(timer)
  if (text.value !== config.value.text) set('text', text.value)
})
</script>

<template>
  <textarea
    v-model="text" :placeholder="t('watchW.note.placeholder')" :aria-label="t('watchW.kind.note')"
    :class="tiny ? 'p-2 text-xs' : 'p-3 text-sm'" class="block size-full resize-none bg-(--ui-warning)/5 leading-relaxed outline-none placeholder:text-(--ui-text-dimmed)"
  />
</template>
