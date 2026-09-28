<script setup lang="ts">
// A script job: what it printed, how it exited, and the agents it called in.
const props = defineProps<{ jobId: string | null }>()
const emit = defineEmits<{ close: [], open: [Job] }>()
const { t, dateLocale } = useLang()
const data = ref<{ job: Job, children: Job[] | null } | null>(null)
watch(() => props.jobId, async (id) => {
  data.value = null
  if (id) data.value = await $fetch(`/api/jobs/${id}`)
})
const open = computed({ get: () => !!props.jobId, set: (v: boolean) => { if (!v) emit('close') } })
const when = (d: string) => new Date(d).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', second: '2-digit', day: '2-digit', month: '2-digit' })
</script>

<template>
  <UModal v-model:open="open" :title="data?.job.title || t('job.detail')" :ui="{ content: 'max-w-3xl' }">
    <template #body>
      <div v-if="data" class="space-y-3 text-sm">
        <div class="flex flex-wrap items-center gap-2 text-xs text-(--ui-text-muted)">
          <JobStatusBadge :status="data.job.status" />
          <span v-if="data.job.exit_code !== null">{{ t('job.exit', { n: data.job.exit_code }) }}</span>
          <span>{{ when(data.job.created_at) }}</span>
          <span v-if="data.job.error" class="text-(--ui-error)">{{ data.job.error }}</span>
        </div>
        <p class="text-xs font-medium text-(--ui-text-muted)">{{ t('job.output') }}</p>
        <pre class="max-h-96 overflow-auto rounded bg-(--ui-bg-elevated) p-2 font-mono text-xs leading-5">{{ data.job.output || t('job.noOutput') }}</pre>
        <template v-if="data.children?.length">
          <p class="text-xs font-medium text-(--ui-text-muted)">{{ t('job.children') }}</p>
          <button
            v-for="c in data.children" :key="c.id" type="button"
            class="flex w-full items-center gap-2 rounded-md border border-(--ui-border) px-3 py-2 text-left hover:bg-(--ui-bg-elevated)/50"
            @click="emit('open', c)"
          >
            <JobStatusBadge :status="c.status" />
            <span class="min-w-0 flex-1 truncate">{{ c.agent_name || c.title }}</span>
            <UIcon name="i-lucide-external-link" class="size-3.5 text-(--ui-text-muted)" />
          </button>
        </template>
      </div>
    </template>
  </UModal>
</template>
