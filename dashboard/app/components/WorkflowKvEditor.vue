<script setup lang="ts">
// Key → value pairs of a step (inputs, headers, outputs): fixed keys are
// given (a sub-workflow's inputs, the workflow's outputs) and cannot be renamed.
const props = defineProps<{ modelValue?: Record<string, string>, fixed?: string[], keyPlaceholder?: string, valuePlaceholder?: string, required?: string[], locked?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [Record<string, string>] }>()
const { t } = useLang()

const rows = computed(() => {
  const m = props.modelValue ?? {}
  const keys = [...(props.fixed ?? []), ...Object.keys(m).filter(k => !props.fixed?.includes(k))]
  return keys.map(k => ({ key: k, value: m[k] ?? '', fixed: !!props.fixed?.includes(k) }))
})
function set(list: { key: string, value: string }[]) {
  emit('update:modelValue', Object.fromEntries(list.map(r => [r.key, r.value])))
}
function setKey(i: number, k: string) {
  const list = rows.value.map(r => ({ ...r }))
  if (list.some((r, j) => j !== i && r.key === k)) return
  list[i]!.key = k
  set(list)
}
function setValue(i: number, v: string) {
  const list = rows.value.map(r => ({ ...r }))
  list[i]!.value = v
  set(list)
}
function add() {
  let k = 'key'
  for (let i = 2; rows.value.some(r => r.key === k); i++) k = `key${i}`
  set([...rows.value, { key: k, value: '' }])
}
const remove = (i: number) => set(rows.value.filter((_, j) => j !== i))
</script>

<template>
  <div class="space-y-1">
    <div v-for="(r, i) in rows" :key="i" class="flex items-center gap-1">
      <UInput
        :model-value="r.key" :disabled="r.fixed" size="xs" class="w-28 shrink-0 font-mono" :placeholder="keyPlaceholder ?? 'key'"
        @update:model-value="v => setKey(i, String(v))"
      />
      <UInput :model-value="r.value" size="xs" class="min-w-0 flex-1 font-mono" :placeholder="valuePlaceholder" :color="required?.includes(r.key) && !r.value.trim() ? 'error' : undefined" :highlight="required?.includes(r.key) && !r.value.trim()" @update:model-value="v => setValue(i, String(v))" />
      <UButton v-if="!r.fixed" size="xs" color="neutral" variant="ghost" icon="i-lucide-x" :aria-label="t('wf.canvas.remove')" @click="remove(i)" />
    </div>
    <UButton v-if="!locked" size="xs" color="neutral" variant="ghost" icon="i-lucide-plus" :label="t('wf.canvas.add')" @click="add()" />
  </div>
</template>
