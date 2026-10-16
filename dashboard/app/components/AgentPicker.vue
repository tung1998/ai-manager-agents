<script setup lang="ts">
const props = withDefaults(defineProps<{
  projectId: string
  modelValue?: string
  compact?: boolean
  iconOnly?: boolean
}>(), { modelValue: '', compact: false, iconOnly: false })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useLang()
const open = ref(false)
const loading = ref(false)
const agents = ref<Agent[]>([])
const defaultAgentId = ref('')

const selected = computed(() => agents.value.find(a => a.id === (props.modelValue || defaultAgentId.value)))
const selectedLabel = computed(() => selected.value?.name ?? t('watchW.agent.default'))

async function load() {
  if (!props.projectId || agents.value.length || loading.value) return
  loading.value = true
  try {
    const res = await $fetch<{ agents: Agent[], default_agent_id: string }>(`/api/projects/${props.projectId}/chat/agents`)
    agents.value = res.agents
    defaultAgentId.value = res.default_agent_id
  } finally {
    loading.value = false
  }
}
watch(open, value => { if (value) void load() })
watch(() => props.projectId, () => { agents.value = []; defaultAgentId.value = '' })

function choose(id: string) {
  emit('update:modelValue', id)
  open.value = false
}
</script>

<template>
  <UModal v-model:open="open" :title="t('watchW.agent.choose')" :ui="{ content: 'max-w-2xl' }">
    <UButton
      :size="compact ? 'xs' : 'sm'" color="neutral" variant="soft"
      :icon="iconOnly ? 'i-lucide-users-round' : undefined"
      :label="iconOnly ? undefined : selectedLabel" :title="t('watchW.agent.change')"
      :aria-label="t('watchW.agent.change')" :disabled="!projectId" :loading="loading"
      @click.stop
    />
    <template #body>
      <div v-if="loading" class="grid gap-3 sm:grid-cols-2"><USkeleton v-for="n in 4" :key="n" class="h-20 rounded-xl" /></div>
      <div v-else-if="!agents.length" class="rounded-xl border border-dashed border-(--ui-border) p-8 text-center text-sm text-(--ui-text-muted)">{{ t('watchW.agent.none') }}</div>
      <div v-else class="grid gap-3 sm:grid-cols-2">
        <button
          type="button" class="flex min-h-24 items-center gap-3 rounded-xl border p-4 text-start transition hover:border-(--ui-primary) hover:bg-(--ui-primary)/5"
          :class="!props.modelValue ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border)'"
          @click="choose('')"
        >
          <span class="grid size-11 shrink-0 place-items-center rounded-full bg-(--ui-bg-elevated)"><UIcon name="i-lucide-users" class="size-5 text-(--ui-text-muted)" /></span>
          <span class="min-w-0"><span class="block font-semibold">{{ t('watchW.agent.default') }}</span><span class="block text-xs text-(--ui-text-muted)">{{ t('watchW.agent.defaultHint') }}</span></span>
          <UIcon v-if="!props.modelValue" name="i-lucide-check" class="ms-auto size-5 shrink-0 text-primary" />
        </button>
        <button
          v-for="a in agents" :key="a.id" type="button"
          class="flex min-h-24 items-center gap-3 rounded-xl border p-4 text-start transition hover:border-(--ui-primary) hover:bg-(--ui-primary)/5"
          :class="props.modelValue === a.id || (!props.modelValue && defaultAgentId === a.id) ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border)'"
          @click="choose(a.id)"
        >
          <AgentAvatar :agent="a" size="md" class="shrink-0" />
          <span class="min-w-0 flex-1"><span class="block truncate font-semibold">{{ a.name }}</span><span class="block truncate text-xs text-(--ui-text-muted)">{{ a.role || a.description || t('watchW.agent.noRole') }}</span><span v-if="a.enabled === false" class="mt-1 block text-xs text-(--ui-error)">{{ t('watchW.agent.paused') }}</span></span>
          <UIcon v-if="props.modelValue === a.id || (!props.modelValue && defaultAgentId === a.id)" name="i-lucide-check" class="size-5 shrink-0 text-primary" />
        </button>
      </div>
    </template>
  </UModal>
</template>
