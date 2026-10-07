<script setup lang="ts">
// A workflow's inputs or outputs (ADR-103): key, description, required, and
// for an output its type.
const props = defineProps<{ modelValue?: WorkflowField[], withType?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [WorkflowField[]] }>()
const { t } = useLang()
const TYPES = ['string', 'number', 'boolean', 'list', 'json'] as const
const list = computed(() => props.modelValue ?? [])

function patch(i: number, p: Partial<WorkflowField>) {
  emit('update:modelValue', list.value.map((f, j) => j === i ? { ...f, ...p } : f))
}
function add() {
  let k = 'value'
  for (let i = 2; list.value.some(f => f.key === k); i++) k = `value${i}`
  emit('update:modelValue', [...list.value, { key: k, required: false }])
}
const remove = (i: number) => emit('update:modelValue', list.value.filter((_, j) => j !== i))
const bad = (k: string) => !k || list.value.filter(f => f.key === k).length > 1
</script>

<template>
  <div class="space-y-1.5">
    <div v-for="(f, i) in list" :key="i" class="space-y-1 rounded-md border border-(--ui-border) p-1.5">
      <div class="flex items-center gap-1">
        <UInput :model-value="f.key" size="xs" class="w-28 shrink-0 font-mono" :color="bad(f.key) ? 'error' : undefined" :highlight="bad(f.key)" @update:model-value="v => patch(i, { key: String(v).trim() })" />
        <USelect v-if="withType" :model-value="f.type || 'string'" :items="[...TYPES]" size="xs" class="w-24" @update:model-value="v => patch(i, { type: v === 'string' ? undefined : v as WorkflowField['type'] })" />
        <UCheckbox :model-value="f.required" :label="t('wf.required')" size="xs" class="ms-auto" @update:model-value="v => patch(i, { required: !!v })" />
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-x" :aria-label="t('wf.canvas.remove')" @click="remove(i)" />
      </div>
      <UInput :model-value="f.description ?? ''" size="xs" class="w-full" :placeholder="t('wf.canvas.description')" @update:model-value="v => patch(i, { description: String(v) || undefined })" />
    </div>
    <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-plus" :label="t('wf.canvas.add')" @click="add()" />
  </div>
</template>
