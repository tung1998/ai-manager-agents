<script setup lang="ts">
const { lang, uiLocale } = useLang()

useHead({
  htmlAttrs: {
    lang
  }
})
</script>

<template>
  <UApp :locale="uiLocale" :toaster="{ position: 'top-right' }">
    <NuxtLayout>
      <NuxtPage />
    </NuxtLayout>
  </UApp>
</template>
