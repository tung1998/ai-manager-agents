<script setup lang="ts">
const { t } = useLang()
const { data } = await useLiveFetch<{ projects: Project[] }>('/api/projects', { lazy: true })
const projects = computed(() => data.value?.projects ?? [])
const projectId = ref('')
const selectedTemplate = ref('')
const projectModalOpen = ref(false)
const search = ref('')
const category = ref('all')
const categories = computed(() => ['all', ...new Set(automationTemplates.map(x => x.category))])
const visibleTemplates = computed(() => automationTemplates.filter(x => {
  const q = search.value.trim().toLowerCase()
  return (category.value === 'all' || x.category === category.value) && (!q || `${x.name} ${x.description} ${x.category}`.toLowerCase().includes(q))
}).sort((a, b) => Number(featuredTemplateKeys.has(b.key)) - Number(featuredTemplateKeys.has(a.key)) || a.name.localeCompare(b.name)))
const projectItems = computed(() => projects.value.map(p => ({ label: p.name, value: p.id })))
function chooseTemplate(key: string) {
  selectedTemplate.value = key
  projectId.value = ''
  projectModalOpen.value = true
}
async function startTemplate() {
  if (!projectId.value || !selectedTemplate.value) return
  await navigateTo(`/projects/${projectId.value}/automations/new?template=${encodeURIComponent(selectedTemplate.value)}`)
}
</script>

<template>
  <PageShell :title="t('nav.automationTemplates')">
    <div class="space-y-4">
      <div class="flex flex-wrap items-end gap-3 rounded-xl border border-(--ui-border) p-4">
        <div class="me-auto min-w-60">
          <p class="text-sm text-(--ui-text-muted)">{{ t('auto.templateHint') }}</p>
        </div>
        <UInput v-model="search" icon="i-lucide-search" :placeholder="t('auto.templateSearch')" class="w-52" />
        <USelect v-model="category" :items="categories.map(x => ({ label: x === 'all' ? t('auto.templateAllCategories') : x, value: x }))" class="w-40" />
      </div>

      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <div v-for="item in visibleTemplates" :key="item.key" class="flex flex-col rounded-xl border border-(--ui-border) p-4">
          <div class="flex items-start gap-3">
            <UIcon :name="item.icon" class="mt-0.5 size-5 shrink-0 text-primary" />
            <div class="min-w-0">
              <p class="flex items-center gap-1.5 font-medium">
                <UIcon v-if="featuredTemplateKeys.has(item.key)" name="i-lucide-star" class="size-3.5 text-(--ui-warning)" :title="t('auto.featuredTemplate')" />
                {{ item.name }}
              </p>
              <p class="mt-1 text-xs text-(--ui-text-muted)">{{ item.category }}</p>
            </div>
          </div>
          <p class="mt-3 flex-1 text-sm text-(--ui-text-muted)">{{ item.description }}</p>
          <UButton class="mt-4 self-start" size="sm" icon="i-lucide-plus" :label="t('auto.useTemplate')" @click="chooseTemplate(item.key)" />
        </div>
      </div>
      <p v-if="!visibleTemplates.length" class="rounded-xl border border-dashed border-(--ui-border) p-8 text-center text-sm text-(--ui-text-muted)">{{ t('auto.templateNoResults') }}</p>
    </div>
  </PageShell>
  <UModal v-model:open="projectModalOpen" :title="t('auto.chooseProjectForTemplateTitle')" :description="t('auto.chooseProjectForTemplateDesc')">
    <template #body>
      <UFormField :label="t('auto.projectPick')" required>
        <USelectMenu v-model="projectId" :items="projectItems" value-key="value" :placeholder="t('auto.projectPick')" :aria-label="t('auto.projectPick')" class="w-full" />
      </UFormField>
      <p v-if="!projects.length" class="mt-3 text-sm text-(--ui-text-muted)">{{ t('auto.chooseProjectEmpty') }}</p>
    </template>
    <template #footer>
      <UButton color="neutral" variant="ghost" :label="t('common.cancel')" @click="projectModalOpen = false" />
      <UButton color="primary" :label="t('auto.startFromTemplate')" :disabled="!projectId" @click="startTemplate" />
    </template>
  </UModal>
</template>
