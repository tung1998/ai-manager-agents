<script setup lang="ts">
// Picks a chat's tags: the project's tags to choose again quickly, or a new
// one typed in. Each change is emitted at once (the caller saves it).
const props = defineProps<{ suggestions: string[] }>()
const model = defineModel<string[]>({ default: () => [] })
const { t } = useLang()
// what is on the chat and what the project has, one list, no repeats
const items = computed(() => {
  const out = [...model.value]
  for (const s of props.suggestions) if (!out.some(x => sameTag(x, s))) out.push(s)
  return out
})
function onCreate(name: string) {
  const tag = name.trim().replace(/\s+/g, ' ')
  if (tag && !model.value.some(x => sameTag(x, tag))) model.value = [...model.value, tag]
}
</script>

<template>
  <UInputMenu
    v-model="model" :items="items" multiple create-item
    :placeholder="t('chatTag.placeholder')" icon="i-lucide-tag" class="w-full" autofocus
    @create="onCreate"
  />
</template>
