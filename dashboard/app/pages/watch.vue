<script setup lang="ts">
// Watch screen (like iTerm): chats of any projects side by side, split right
// or below, each box kept in this browser.
const { t } = useLang()
const { root, add } = useWatchLayout()
const { data: proj } = useLiveFetch<{ projects: Project[] }>('/api/projects', { lazy: true, server: false })
const projects = computed(() => proj.value?.projects ?? [])
</script>

<template>
  <!-- the page's usual header; boxes are added and closed from the boxes themselves -->
  <PageShell :title="t('watch.title')" body="p-2 sm:p-2 min-h-0 overflow-hidden">
    <div v-if="!proj" class="grid h-full grid-cols-2 gap-2" aria-busy="true">
      <USkeleton class="h-full" /><USkeleton class="h-full" />
    </div>
    <WatchTree v-else-if="root" :node="root" :projects="projects" class="h-full" />
    <div v-else class="flex h-full items-start justify-center pt-[8vh]">
      <WatchPicker :projects="projects" :hint="t('watch.empty')" class="max-h-[70vh] w-full max-w-lg" @pick="add" />
    </div>
  </PageShell>
</template>
