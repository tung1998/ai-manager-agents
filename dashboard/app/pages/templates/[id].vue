<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <PageShell title="Mô hình mẫu">
    <template #actions>
      <UButton to="/templates" icon="i-lucide-arrow-left" label="Tất cả mẫu" color="neutral" variant="ghost" />
    </template>
    <OrgModelEditor :key="id" :model-id="id" />
  </PageShell>
</template>
