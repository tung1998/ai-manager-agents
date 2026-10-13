<script setup lang="ts">
// A model name: typed freely, with the provider's models as suggestions.
// Replaces the browser's <datalist> (native arrow, spellcheck line, an
// unstyled popup that showed only the exact match once a model was set).
const props = defineProps<{ models?: string[], placeholder?: string, size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl', disabled?: boolean }>()
const model = defineModel<string>({ default: '' })
const search = ref(model.value)
watch(model, (v) => { if (v !== search.value) search.value = v ?? '' })
// the whole list while the text is a known model (just opened); filtered while typing
const items = computed(() => {
  const all = props.models ?? []
  const q = search.value.trim().toLowerCase()
  if (!q || all.includes(search.value.trim())) return all
  return all.filter(m => m.toLowerCase().includes(q))
})
</script>

<template>
  <UInputMenu
    v-model="model" v-model:search-term="search" mode="autocomplete" :items="items" ignore-filter open-on-focus open-on-click
    :placeholder="props.placeholder" :size="props.size" :disabled="props.disabled"
    :trailing-icon="props.models?.length ? undefined : ''" spellcheck="false" autocomplete="off"
    :ui="{ base: 'font-mono', item: 'font-mono' }"
  />
</template>
