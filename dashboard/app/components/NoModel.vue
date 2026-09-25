<script setup lang="ts">
defineProps<{ projectId: string, admin: boolean }>()
defineEmits<{ choose: [] }>()
</script>

<template>
  <div class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
    <UIcon name="i-lucide-network" class="mx-auto size-8 text-(--ui-text-dimmed)" />
    <p class="mt-2 font-medium">Project chưa có mô hình tổ chức</p>
    <p class="text-sm text-(--ui-text-muted)">Cần mô hình để chat và giao việc. Để AI đề xuất, hoặc tự chọn Solo, Team, Tam quyền.</p>
    <div v-if="admin" class="mt-4 flex justify-center gap-2">
      <UButton :to="`/projects/${projectId}/setup`" icon="i-lucide-sparkles" label="Thiết lập bằng AI" />
      <UButton icon="i-lucide-network" label="Tự chọn mô hình" color="neutral" variant="outline" @click="$emit('choose')" />
    </div>
  </div>
</template>
