<script setup lang="ts">
// The AI connections' usage windows (5 hours, the week…): the providers page's bars in a box.
const { t } = useLang()
const { limits, refresh } = useLimits()
const loaded = ref(false)
onMounted(async () => {
  await refresh(true)
  loaded.value = true
})
useWatchPoll(() => refresh(true), 60)
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto p-3">
    <LoadingRows v-if="!loaded && !limits.length" :n="2" />
    <WatchEmpty v-else-if="!limits.length" icon="i-lucide-gauge" :text="t('watchW.limits.none')" />
    <UsageLimits v-else />
  </div>
</template>
