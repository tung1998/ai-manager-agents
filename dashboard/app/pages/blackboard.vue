<script setup lang="ts">
const { t } = useLang()
</script>

<template>
  <ComingSoon :title="t('blackboard.title')" icon="i-lucide-messages-square" milestone="M4" :description="t('blackboard.desc')" />
</template>
