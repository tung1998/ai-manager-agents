<script setup lang="ts">
// Watch screen (like Graylog's dashboards, laid out like a phone's home
// screen): several boards, each a grid of boxes (1×1 … 12×12 cells), each box
// a widget (a chat, a process, a monitor, a figure…); kept in this browser.
const { t } = useLang()
const { count, add, board } = useWatchLayout()
const { data: proj } = useLiveFetch<{ projects: Project[] }>('/api/projects', { lazy: true, server: false })
const projects = computed(() => proj.value?.projects ?? [])
</script>

<template>
  <!-- the page's usual header; boards and boxes are handled from their own controls -->
  <PageShell :title="t('watch.title')" body="p-2 sm:p-2 min-h-0 overflow-hidden flex flex-col gap-2">
    <WatchTabs :projects="projects" class="shrink-0" />
    <div v-if="!proj" class="grid min-h-0 flex-1 grid-cols-2 gap-2" aria-busy="true">
      <USkeleton class="h-full" /><USkeleton class="h-full" />
    </div>
    <WatchGrid v-else-if="count" :key="board?.id" :projects="projects" class="min-h-0 flex-1" />
    <div v-else class="flex min-h-0 flex-1 items-start justify-center pt-[6vh]">
      <WatchPicker
        :projects="projects" :hint="t('watch.empty')" class="max-h-[75vh] w-full max-w-2xl"
        @pick="(p, c) => add(p, c)" @widget="(k) => add(board?.projectId ?? '', undefined, k)"
      />
    </div>
  </PageShell>
</template>
