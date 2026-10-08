<script setup lang="ts">
// How agents speak (ADR-121): office's language, and the one they answer the
// person in (auto: the language the person writes in). Among themselves: English.
const { t } = useLang()
const toast = useToast()
const { data, refresh } = useLiveFetch<{ system: string, response: string, languages: Record<string, string> }>('/api/language', { lazy: true })
const form = reactive({ system: 'vi', response: 'auto' })
const { stale, reset: resync } = useDraft(data, form, d => Object.assign(form, { system: d.system, response: d.response }))
const responseItems = computed(() => [{ value: 'auto', label: t('lang.auto') }, ...Object.entries(data.value?.languages ?? {}).map(([value, label]) => ({ value, label }))])
const systemItems = computed(() => ['vi', 'en'].map(v => ({ value: v, label: data.value?.languages[v] ?? v })))
const saving = ref(false)
async function save() {
  saving.value = true
  try {
    await $fetch('/api/language', { method: 'PUT', body: { ...form } })
    await refresh()
    resync()
    toast.add({ title: t('lang.saved'), color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <UCard :ui="{ body: 'space-y-3 sm:p-4' }">
    <div class="flex items-center gap-2">
      <UIcon name="i-lucide-languages" class="size-4 text-(--ui-text-muted)" />
      <p class="font-medium">{{ t('lang.title') }}</p>
      <UTooltip :text="t('lang.help')"><UIcon name="i-lucide-info" class="size-4 text-(--ui-text-muted)" /></UTooltip>
    </div>
    <StaleNotice :show="stale" @reload="resync" />
    <USkeleton v-if="!data" class="h-10 w-full" />
    <div v-else class="grid gap-3 sm:grid-cols-[1fr_1fr_auto] sm:items-end">
      <UFormField :label="t('lang.response')">
        <USelect v-model="form.response" :items="responseItems" class="w-full" />
      </UFormField>
      <UFormField :label="t('lang.system')">
        <USelect v-model="form.system" :items="systemItems" class="w-full" />
      </UFormField>
      <UButton icon="i-lucide-save" :label="t('common.save')" :loading="saving" @click="save" />
    </div>
  </UCard>
</template>
