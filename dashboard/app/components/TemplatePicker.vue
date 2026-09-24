<script setup lang="ts">
defineProps<{ templates: OrgModel[], allowAi?: boolean }>()
const selected = defineModel<string>({ default: '' })
</script>

<template>
  <div class="grid gap-2">
    <button
      v-if="allowAi" type="button"
      class="flex items-start gap-3 rounded-lg border p-3 text-left transition"
      :class="selected === '__ai' ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
      @click="selected = '__ai'"
    >
      <UIcon name="i-lucide-sparkles" class="mt-0.5 size-5 shrink-0 text-primary" />
      <div class="min-w-0">
        <p class="font-medium">AI đề xuất <span class="text-xs font-normal text-(--ui-text-muted)">· khuyên dùng</span></p>
        <p class="text-sm text-(--ui-text-muted)">Quét project, đọc CLAUDE.md, AGENTS.md, .claude/agents… rồi chọn mô hình và tinh chỉnh agent. Cần kết nối AI.</p>
      </div>
    </button>
    <button
      v-for="t in templates" :key="t.id" type="button"
      class="flex items-start gap-3 rounded-lg border p-3 text-left transition"
      :class="selected === t.id ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
      @click="selected = t.id"
    >
      <UIcon :name="kindIcon[t.kind]" class="mt-0.5 size-5 shrink-0 text-primary" />
      <div class="min-w-0">
        <p class="font-medium">
          {{ t.name }} <span class="text-xs font-normal text-(--ui-text-muted)">· {{ t.agent_count }} agent</span>
        </p>
        <p class="line-clamp-2 text-sm text-(--ui-text-muted)">{{ t.description }}</p>
      </div>
    </button>
  </div>
</template>
