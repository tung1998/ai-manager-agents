<script setup lang="ts">
// A new webhook secret, shown once: only the "I saved it" button closes it.
const props = defineProps<{ value: { url: string, secret: string } | null }>()
const emit = defineEmits<{ close: [] }>()
const toast = useToast()
const { t } = useLang()
const open = computed(() => !!props.value)
const curl = computed(() => props.value
  ? `curl -X POST -H 'Authorization: Bearer ${props.value.secret}' -H 'Content-Type: application/json' -d '{"hello":"world"}' ${props.value.url}`
  : '')
async function copy(text: string) {
  await navigator.clipboard.writeText(text)
  toast.add({ title: t('auto.copied'), color: 'success' })
}
</script>

<template>
  <UModal :open="open" :dismissible="false" :close="false" :title="t('auto.secretTitle')" :description="t('auto.secretDesc')">
    <template #body>
      <div v-if="value" class="space-y-3 text-sm">
        <div v-for="row in [{ label: t('auto.url'), text: value.url }, { label: t('auto.secret'), text: value.secret }, { label: t('auto.curl'), text: curl }]" :key="row.label" class="space-y-1">
          <p class="text-xs font-medium text-(--ui-text-muted)">{{ row.label }}</p>
          <div class="flex items-start gap-2">
            <code class="min-w-0 flex-1 break-all rounded bg-(--ui-bg-elevated) px-2 py-1 text-xs">{{ row.text }}</code>
            <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-copy" :aria-label="t('auto.copy')" @click="copy(row.text)" />
          </div>
        </div>
      </div>
    </template>
    <template #footer>
      <UButton :label="t('auto.savedIt')" @click="emit('close')" />
    </template>
  </UModal>
</template>
