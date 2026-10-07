<script setup lang="ts">
// The office's workflows (ADR-098): files a project copies when it installs
// one. The shipped ones come back as they were with "reset".
const toast = useToast()
const { t } = useLang()
const { data, refresh, pending } = useLiveFetch<{ workflows: LibraryWorkflow[] }>('/api/workflow-library', { lazy: true })
const list = computed(() => [...(data.value?.workflows ?? [])].sort((a, b) => Number(b.builtin) - Number(a.builtin) || a.def.key.localeCompare(b.def.key)))

const editTo = (key?: string) => ({ path: '/workflows/edit', query: key ? { key } : {} })
async function reset(w: LibraryWorkflow) {
  if (!confirm(t('wf.resetConfirm', { name: w.def.name || w.def.key }))) return
  try {
    await $fetch(`/api/workflow-library/${w.def.key}/reset`, { method: 'POST', body: {} })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
async function remove(w: LibraryWorkflow) {
  if (!confirm(t('wf.deleteConfirm', { name: w.def.name || w.def.key }))) return
  try {
    await $fetch(`/api/workflow-library/${w.def.key}`, { method: 'DELETE' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-2">
      <p class="text-sm text-(--ui-text-muted)">{{ t('wf.count', { n: list.length }) }}</p>
      <UTooltip :text="t('wf.libraryInfo')"><UIcon name="i-lucide-info" class="size-4 text-(--ui-text-muted)" /></UTooltip>
      <UButton class="ms-auto" size="sm" icon="i-lucide-plus" :label="t('wf.new')" :to="editTo()" />
    </div>
    <LoadingRows v-if="pending && !data" />
    <div v-else-if="!list.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
      <UIcon name="i-lucide-workflow" class="mx-auto size-8 text-(--ui-text-dimmed)" />
      <p class="mt-2 font-medium">{{ t('wf.libraryEmpty') }}</p>
    </div>
    <div v-else class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
      <div v-for="w in list" :key="w.def.key" class="flex items-center gap-3 px-4 py-2.5">
        <UIcon name="i-lucide-workflow" class="size-4 shrink-0 text-primary" />
        <div class="min-w-0 flex-1">
          <p class="flex flex-wrap items-center gap-2">
            <NuxtLink :to="editTo(w.def.key)" class="text-sm font-medium hover:text-primary">{{ w.def.name || w.def.key }}</NuxtLink>
            <span class="font-mono text-xs text-(--ui-text-muted)">/{{ w.def.key }}</span>
            <UBadge v-if="w.builtin" color="neutral" variant="outline" size="sm" :label="t('wf.builtin')" />
            <UBadge v-if="w.modified" color="warning" variant="subtle" size="sm" :label="t('wf.modified')" />
            <UBadge v-if="w.error" color="error" variant="subtle" size="sm" icon="i-lucide-circle-alert" :label="t('wf.invalid')" :title="w.error" />
          </p>
          <p v-if="w.error" class="line-clamp-1 text-xs text-(--ui-error)">{{ w.error }}</p>
          <p v-else-if="w.def.description" class="line-clamp-1 text-xs text-(--ui-text-muted)">{{ w.def.description }}</p>
        </div>
        <span v-if="w.def.roles?.length" class="hidden shrink-0 items-center gap-1 text-xs text-(--ui-text-muted) sm:flex" :title="w.def.roles.map(r => r.name).join(', ')">
          <UIcon v-for="r in w.def.roles" :key="r.key" :name="accessIcon[r.access] ?? 'i-lucide-eye'" class="size-3.5" />
        </span>
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-pencil" :label="t('wf.edit')" :to="editTo(w.def.key)" />
        <UButton v-if="w.builtin && w.modified" size="xs" color="neutral" variant="ghost" icon="i-lucide-rotate-ccw" :aria-label="t('wf.reset')" :title="t('wf.reset')" @click="reset(w)" />
        <UButton v-else-if="!w.builtin" size="xs" color="error" variant="ghost" icon="i-lucide-trash-2" :aria-label="t('wf.delete')" :title="t('wf.delete')" @click="remove(w)" />
      </div>
    </div>
  </div>
</template>
