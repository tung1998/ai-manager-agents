<script setup lang="ts">
const route = useRoute()
const { t } = useLang()
const projectId = computed(() => route.params.id as string)
const { data: savedData } = useLiveFetch<{ items: SavedAutomationTemplate[] }>('/api/automation/templates')
const templates = computed(() => [...automationTemplates, ...(savedData.value?.items ?? []).map(automationTemplateFromLibrary)])
// ?template=... selects a starter without saving it. Keep ?preset=pr for old
// links and bookmarks.
const selectedKey = computed(() => String(route.query.template ?? (route.query.preset === 'pr' ? 'pr-review' : '')))
const selectedTemplate = computed(() => templates.value.find(x => x.key === selectedKey.value))
const preset = computed(() => selectedTemplate.value?.make() ?? undefined)
const queryFor = (key: string) => ({ query: { template: key } })
const libraryOpen = ref(false)
const search = ref('')
const category = ref('all')
const categories = computed(() => ['all', ...new Set(templates.value.map(x => x.category))])
const visibleTemplates = computed(() => templates.value.filter(x => {
  const q = search.value.trim().toLowerCase()
  return (category.value === 'all' || x.category === category.value) && (!q || `${x.name} ${x.description} ${x.category}`.toLowerCase().includes(q))
}).sort((a, b) => Number(featuredTemplateKeys.has(b.key)) - Number(featuredTemplateKeys.has(a.key)) || a.name.localeCompare(b.name)))
</script>

<template>
  <PageShell :title="t('auto.new')">
    <UButton :to="{ path: `/projects/${projectId}`, query: { tab: 'automations' } }" icon="i-lucide-arrow-left" size="xs" color="neutral" variant="ghost" class="-ms-2 mb-2" :label="t('auto.back')" />
    <!-- the library opens in a modal; the page shows only what was picked -->
    <div class="mb-4 flex flex-wrap items-center gap-2">
      <span class="text-sm font-medium">{{ t('auto.templates') }}</span>
      <UButton
        color="neutral" variant="outline" trailing-icon="i-lucide-chevron-down"
        :icon="selectedTemplate?.icon ?? 'i-lucide-library'" :label="selectedTemplate?.name ?? t('auto.startFromTemplate')"
        @click="libraryOpen = true"
      />
    </div>
    <UModal v-model:open="libraryOpen" :title="t('auto.templateLibraryTitle')" :description="t('auto.templateLibraryDesc')" :ui="{ content: 'sm:max-w-4xl', body: 'max-h-[min(75dvh,48rem)] overflow-y-auto' }">
      <template #body>
        <div class="flex flex-wrap items-center gap-2">
          <UInput v-model="search" size="sm" icon="i-lucide-search" :placeholder="t('auto.templateSearch')" class="w-52" autofocus />
          <USelect v-model="category" size="sm" :items="categories.map(x => ({ label: x === 'all' ? t('auto.templateAllCategories') : x, value: x }))" class="w-40" />
          <span class="ms-auto text-xs text-(--ui-text-muted)">{{ t('auto.templateCount', { n: visibleTemplates.length }) }}</span>
        </div>
        <div class="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          <NuxtLink
            v-for="item in visibleTemplates" :key="item.key" :to="queryFor(item.key)"
            class="flex items-start gap-2 rounded-lg border p-3 transition hover:border-primary"
            :class="selectedKey === item.key ? 'border-primary bg-primary/5' : 'border-(--ui-border)'"
            @click="libraryOpen = false"
          >
            <UIcon :name="item.icon" class="mt-0.5 size-4 shrink-0 text-primary" />
            <span class="min-w-0">
              <span class="flex items-center gap-1.5 text-sm font-medium">
                <UIcon v-if="featuredTemplateKeys.has(item.key)" name="i-lucide-star" class="size-3.5 text-(--ui-warning)" :title="t('auto.featuredTemplate')" />
                {{ item.name }}
              </span>
              <span class="mt-0.5 block line-clamp-2 text-xs text-(--ui-text-muted)">{{ item.description }}</span>
            </span>
          </NuxtLink>
        </div>
        <p v-if="!visibleTemplates.length" class="mt-3 text-sm text-(--ui-text-muted)">{{ t('auto.templateNoResults') }}</p>
      </template>
    </UModal>
    <AutomationBuilder :key="`${selectedKey}:${!!preset}`" :project-id="projectId" :preset="preset" />
  </PageShell>
</template>
