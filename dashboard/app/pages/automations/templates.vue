<script setup lang="ts">
const { t } = useLang()
const { data } = await useLiveFetch<{ projects: Project[] }>('/api/projects', { lazy: true })
const projects = computed(() => data.value?.projects ?? [])
const projectId = ref('')
const projectItems = computed(() => projects.value.map(project => ({ label: project.name, value: project.id })))
const continueToTemplates = computed(() => projectId.value ? `/projects/${projectId.value}/automations/new` : undefined)
</script>

<template>
  <PageShell :title="t('nav.automationTemplates')">
    <div class="mx-auto max-w-xl">
      <div class="rounded-2xl border border-(--ui-border) bg-(--ui-bg-elevated) p-6 shadow-sm sm:p-8">
        <div class="mb-6 flex size-12 items-center justify-center rounded-xl bg-primary/10 text-primary">
          <UIcon name="i-lucide-library-big" class="size-6" />
        </div>
        <h2 class="text-lg font-semibold">{{ t('auto.chooseProjectTitle') }}</h2>
        <p class="mt-1 text-sm leading-6 text-(--ui-text-muted)">{{ t('auto.chooseProjectDesc') }}</p>

        <UFormField class="mt-6" :label="t('auto.projectPick')" required>
          <USelectMenu v-model="projectId" :items="projectItems" value-key="value" :placeholder="t('auto.projectPick')" :aria-label="t('auto.projectPick')" class="w-full" />
        </UFormField>
        <UButton class="mt-5 w-full justify-center" icon="i-lucide-arrow-right" trailing :label="t('auto.chooseProjectContinue')" :to="continueToTemplates" :disabled="!projectId" />
      </div>
      <p v-if="!projects.length" class="mt-4 text-center text-sm text-(--ui-text-muted)">{{ t('auto.chooseProjectEmpty') }}</p>
    </div>
  </PageShell>
</template>
