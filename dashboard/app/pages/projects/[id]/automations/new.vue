<script setup lang="ts">
const route = useRoute()
const { t } = useLang()
const projectId = computed(() => route.params.id as string)
// ?template=... selects a starter without saving it. Keep ?preset=pr for old
// links and bookmarks.
const selectedKey = computed(() => String(route.query.template ?? (route.query.preset === 'pr' ? 'pr-review' : '')))
const selectedTemplate = computed(() => automationTemplates.find(x => x.key === selectedKey.value))
const preset = computed(() => selectedTemplate.value?.make() ?? undefined)
const queryFor = (key: string) => ({ query: { template: key } })
const search = ref('')
const category = ref('all')
const categories = computed(() => ['all', ...new Set(automationTemplates.map(x => x.category))])
const visibleTemplates = computed(() => automationTemplates.filter(x => {
  const q = search.value.trim().toLowerCase()
  return (category.value === 'all' || x.category === category.value) && (!q || `${x.name} ${x.description} ${x.category}`.toLowerCase().includes(q))
}).sort((a, b) => Number(featuredTemplateKeys.has(b.key)) - Number(featuredTemplateKeys.has(a.key)) || a.name.localeCompare(b.name)))
</script>

<template>
  <PageShell :title="t('auto.new')">
    <UButton :to="{ path: `/projects/${projectId}`, query: { tab: 'automations' } }" icon="i-lucide-arrow-left" size="xs" color="neutral" variant="ghost" class="-ms-2 mb-2" :label="t('auto.back')" />
    <div class="mb-5 rounded-2xl border border-(--ui-border) bg-(--ui-bg-elevated) p-4 shadow-sm sm:p-5">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div class="min-w-0 flex-1">
          <p class="text-sm font-semibold">{{ t('auto.templateLibraryTitle') }}</p>
          <p class="mt-0.5 text-xs text-(--ui-text-muted)">{{ t('auto.templateLibraryDesc') }}</p>
        </div>
        <UInput v-model="search" size="sm" icon="i-lucide-search" :placeholder="t('auto.templateSearch')" class="w-full sm:w-64" />
        <USelect v-model="category" size="sm" :items="categories.map(x => ({ label: x === 'all' ? t('auto.templateAllCategories') : x, value: x }))" :aria-label="t('auto.templateAllCategories')" class="w-full sm:w-44" />
      </div>
      <p class="mt-3 text-xs text-(--ui-text-muted)">{{ t('auto.templateCount', { n: visibleTemplates.length }) }}</p>
      <div class="mt-3 divide-y divide-(--ui-border) rounded-xl border border-(--ui-border)">
        <NuxtLink
          v-for="item in visibleTemplates" :key="item.key" :to="queryFor(item.key)"
          class="group flex items-center gap-3 px-3 py-2.5 transition hover:bg-primary/5 sm:px-4"
          :class="selectedKey === item.key ? 'bg-primary/5' : ''"
        >
          <UIcon :name="item.icon" class="size-4 shrink-0 text-primary" />
          <span class="min-w-0 flex-1 truncate text-sm font-medium">
            <UIcon v-if="featuredTemplateKeys.has(item.key)" name="i-lucide-star" class="me-1 inline-block size-3.5 align-[-2px] text-(--ui-warning)" :title="t('auto.featuredTemplate')" />
            {{ item.name }}
          </span>
          <span class="hidden max-w-40 truncate text-xs text-(--ui-text-muted) sm:block">{{ item.category }}</span>
          <UIcon name="i-lucide-chevron-right" class="size-4 shrink-0 text-(--ui-text-muted) transition group-hover:translate-x-0.5 group-hover:text-primary" />
        </NuxtLink>
      </div>
      <p v-if="!visibleTemplates.length" class="mt-3 text-sm text-(--ui-text-muted)">{{ t('auto.templateNoResults') }}</p>
    </div>
    <AutomationBuilder :key="selectedKey" :project-id="projectId" :preset="preset" />
  </PageShell>
</template>
