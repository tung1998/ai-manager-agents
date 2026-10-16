<script setup lang="ts">
// The office's workflows (ADR-098): files a project copies when it installs
// one. The shipped ones come back as they were with "reset".
const toast = useToast()
const { t } = useLang()
const { data, refresh, pending } = useLiveFetch<{ workflows: LibraryWorkflow[] }>('/api/workflow-library', { lazy: true })
const search = ref('')
const category = ref('all')
const featuredWorkflowKeys = new Set(['review-pr', 'bugfix', 'incident-response', 'product-discovery', 'security-assessment', 'launch-readiness', 'write-content', 'customer-onboarding', 'data-quality-review', 'sales-deal-review'])
const categoryOf = (w: LibraryWorkflow) => w.def.category || 'Other'
const list = computed(() => [...(data.value?.workflows ?? [])].sort((a, b) =>
  Number(featuredWorkflowKeys.has(b.def.key)) - Number(featuredWorkflowKeys.has(a.def.key))
  || Number(b.builtin) - Number(a.builtin)
  || a.def.key.localeCompare(b.def.key)))
const categories = computed(() => ['all', ...new Set(list.value.map(categoryOf))])
const filteredList = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  if (!query && category.value === 'all') return list.value
  return list.value.filter(w => (category.value === 'all' || categoryOf(w) === category.value) && [w.def.name, w.def.key, w.def.description, categoryOf(w), ...(w.def.roles ?? []).map(r => r.name)].join(' ').toLocaleLowerCase().includes(query))
})

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
    <div class="rounded-2xl border border-(--ui-border) bg-(--ui-bg-elevated) p-4 shadow-sm">
      <div class="flex flex-wrap items-center gap-3">
        <div class="min-w-0">
          <p class="font-medium">{{ t('wf.library') }}</p>
          <p class="text-sm text-(--ui-text-muted)">{{ t('wf.libraryInfo') }}</p>
        </div>
        <InfoTip :text="t('wf.libraryInfo')" icon-class="size-4 text-(--ui-text-muted)" />
        <UButton class="ms-auto" size="sm" icon="i-lucide-plus" :label="t('wf.new')" :to="editTo()" />
      </div>
      <div class="mt-4 flex flex-wrap items-center gap-3">
        <UInput v-model="search" class="min-w-56 flex-1" icon="i-lucide-search" :placeholder="t('wf.search')" :aria-label="t('wf.search')" />
        <USelect v-model="category" size="sm" :items="categories.map(x => ({ label: x === 'all' ? t('wf.allCategories') : x === 'Other' ? t('wf.otherCategory') : x, value: x }))" :aria-label="t('wf.allCategories')" class="w-full sm:w-48" />
        <p class="shrink-0 text-sm text-(--ui-text-muted)">{{ t('wf.countFiltered', { shown: filteredList.length, total: list.length }) }}</p>
      </div>
    </div>
    <LoadingRows v-if="pending && !data" />
    <div v-else-if="!list.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
      <UIcon name="i-lucide-workflow" class="mx-auto size-8 text-(--ui-text-dimmed)" />
      <p class="mt-2 font-medium">{{ t('wf.libraryEmpty') }}</p>
    </div>
    <div v-else-if="!filteredList.length" class="rounded-xl border border-dashed border-(--ui-border) p-10 text-center">
      <UIcon name="i-lucide-search-x" class="mx-auto size-8 text-(--ui-text-dimmed)" />
      <p class="mt-2 font-medium">{{ t('wf.searchEmpty') }}</p>
      <UButton class="mt-3" size="sm" color="neutral" variant="soft" :label="t('wf.clearSearch')" @click="search = ''" />
    </div>
    <div v-else class="divide-y divide-(--ui-border) overflow-hidden rounded-xl border border-(--ui-border) bg-(--ui-bg-elevated)">
      <div v-for="w in filteredList" :key="w.def.key" class="group flex items-center gap-3 px-4 py-3 transition-colors hover:bg-(--ui-bg-accented)">
        <UIcon name="i-lucide-workflow" class="size-4 shrink-0 text-primary" />
        <div class="min-w-0 flex-1">
          <p class="flex flex-wrap items-center gap-2">
            <UIcon v-if="featuredWorkflowKeys.has(w.def.key)" name="i-lucide-star" class="size-3.5 text-(--ui-warning)" :title="t('wf.featured')" />
            <NuxtLink :to="editTo(w.def.key)" class="text-sm font-medium hover:text-primary">{{ w.def.name || w.def.key }}</NuxtLink>
            <span class="font-mono text-xs text-(--ui-text-dimmed)">#{{ w.def.key }}</span>
            <span class="hidden text-xs text-(--ui-text-muted) sm:inline">{{ categoryOf(w) === 'Other' ? t('wf.otherCategory') : categoryOf(w) }}</span>
            <UBadge v-if="w.builtin" color="neutral" variant="outline" size="sm" :label="t('wf.builtin')" />
            <UBadge v-if="w.modified" color="warning" variant="subtle" size="sm" :label="t('wf.modified')" />
            <UBadge v-if="w.error" color="error" variant="subtle" size="sm" icon="i-lucide-circle-alert" :label="t('wf.invalid')" :title="w.error" />
          </p>
          <p v-if="w.error" class="line-clamp-1 text-xs text-(--ui-error)">{{ w.error }}</p>
          <p v-else-if="w.def.description" class="line-clamp-1 text-xs text-(--ui-text-muted)">{{ w.def.description }}</p>
        </div>
        <span v-if="w.def.roles?.length" class="hidden shrink-0 items-center gap-1 text-xs text-(--ui-text-muted) sm:flex" :title="w.def.roles.map(r => r.name).join(', ')">
          <UIcon v-for="r in w.def.roles" :key="r.key" :name="accessIcon[accessKey(r.access)]" class="size-3.5" />
        </span>
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-pencil" :label="t('wf.edit')" :to="editTo(w.def.key)" />
        <UButton v-if="w.builtin && w.modified" size="xs" color="neutral" variant="ghost" icon="i-lucide-rotate-ccw" :aria-label="t('wf.reset')" :title="t('wf.reset')" @click="reset(w)" />
        <UButton v-else-if="!w.builtin" size="xs" color="error" variant="ghost" icon="i-lucide-trash-2" :aria-label="t('wf.delete')" :title="t('wf.delete')" @click="remove(w)" />
      </div>
    </div>
  </div>
</template>
