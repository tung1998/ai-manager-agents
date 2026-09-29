<script setup lang="ts">
// Filters a list by where its items came from ('all' = every source).
const model = defineModel<'all' | Source>({ default: 'all' })
const { t } = useLang()
const items = computed(() => [
  { label: t('source.all'), value: 'all', icon: 'i-lucide-layers' },
  ...sources.map(s => ({ label: t(`source.${s}`), value: s, icon: sourceIcon[s] }))
])
</script>

<template>
  <USelect v-model="model" :items="items" size="xs" variant="ghost" class="w-28 shrink-0" :aria-label="t('source.filter')" />
</template>
