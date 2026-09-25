<script setup lang="ts">
const { t } = useLang()
</script>

<template>
  <ComingSoon :title="t('incidents.title')" icon="i-lucide-siren" milestone="M4" :description="t('incidents.desc')" />
</template>
