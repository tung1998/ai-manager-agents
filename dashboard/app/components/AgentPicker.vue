<script setup lang="ts">
const props = withDefaults(defineProps<{
  projectId: string
  modelValue?: string
  selectedAgent?: Agent
  compact?: boolean
  iconOnly?: boolean
}>(), { modelValue: '', compact: false, iconOnly: false })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useLang()
const open = ref(false)
const loading = ref(false)
const agents = ref<Agent[]>([])
const defaultAgentId = ref('')
const query = ref('')

const selected = computed(() => props.selectedAgent ?? agents.value.find(a => a.id === props.modelValue))
const selectedLabel = computed(() => selected.value?.name ?? (props.modelValue ? t('watchW.agent.selected') : t('watchW.agent.default')))
const activeAgents = computed(() => filteredAgents.value.filter(a => a.enabled !== false))
const pausedAgents = computed(() => filteredAgents.value.filter(a => a.enabled === false))
const filteredAgents = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  if (!needle) return agents.value
  return agents.value.filter(a => [a.name, a.role, a.description].some(value => value?.toLocaleLowerCase().includes(needle)))
})

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
watch(open, value => {
  if (value) {
    query.value = ''
    void load()
  }
})
watch(() => props.projectId, () => { agents.value = []; defaultAgentId.value = '' })

function choose(id: string) {
  emit('update:modelValue', id)
  open.value = false
}

function isSelected(id: string) {
  return props.modelValue === id || (!props.modelValue && defaultAgentId.value === id)
}
</script>

<template>
  <UModal v-model:open="open" :title="t('watchW.agent.choose')" :description="t('watchW.agent.chooseHint')" :ui="{ content: 'max-w-3xl' }">
    <UButton
      :size="compact ? 'xs' : 'sm'" color="neutral" variant="soft" class="max-w-full"
      :title="t('watchW.agent.change')" :aria-label="`${t('watchW.agent.change')}: ${selectedLabel}`"
      :disabled="!projectId" :loading="loading"
      @click.stop
    >
      <AgentAvatar v-if="selected" :agent="selected" :size="compact ? 'xs' : 'sm'" />
      <UIcon v-else name="i-lucide-bot" class="size-4 shrink-0" />
      <span v-if="!iconOnly" class="truncate">{{ selectedLabel }}</span>
      <UIcon v-if="!iconOnly" name="i-lucide-chevrons-up-down" class="ms-auto size-3.5 shrink-0 opacity-60" />
    </UButton>
    <template #body>
      <div v-if="loading" class="grid gap-3 sm:grid-cols-2"><USkeleton v-for="n in 4" :key="n" class="h-20 rounded-xl" /></div>
      <div v-else-if="!agents.length" class="rounded-xl border border-dashed border-(--ui-border) p-8 text-center text-sm text-(--ui-text-muted)">{{ t('watchW.agent.none') }}</div>
      <div v-else class="space-y-5">
        <UInput v-model="query" icon="i-lucide-search" :placeholder="t('watchW.agent.search')" class="w-full" />
        <button
          type="button" :aria-pressed="!props.modelValue" class="flex min-h-24 w-full items-center gap-3 rounded-xl border p-4 text-start transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--ui-primary) hover:border-(--ui-primary) hover:bg-(--ui-primary)/5"
          :class="!props.modelValue ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border)'"
          @click="choose('')"
        >
          <span class="grid size-11 shrink-0 place-items-center rounded-full bg-(--ui-bg-elevated)"><UIcon name="i-lucide-users" class="size-5 text-(--ui-text-muted)" /></span>
          <span class="min-w-0"><span class="block font-semibold">{{ t('watchW.agent.default') }}</span><span class="block text-xs text-(--ui-text-muted)">{{ t('watchW.agent.defaultHint') }}</span></span>
          <UIcon v-if="!props.modelValue" name="i-lucide-check" class="ms-auto size-5 shrink-0 text-primary" />
        </button>
        <section v-if="activeAgents.length" class="space-y-2">
          <h3 class="text-xs font-semibold uppercase tracking-wide text-(--ui-text-muted)">{{ t('watchW.agent.available') }}</h3>
          <div class="grid gap-3 sm:grid-cols-2">
            <button
              v-for="a in activeAgents" :key="a.id" type="button" :aria-pressed="isSelected(a.id)"
              class="flex min-h-24 items-center gap-3 rounded-xl border p-4 text-start transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--ui-primary) hover:border-(--ui-primary) hover:bg-(--ui-primary)/5"
              :class="isSelected(a.id) ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border)'"
              @click="choose(a.id)"
            >
              <AgentAvatar :agent="a" size="md" class="shrink-0" />
              <span class="min-w-0 flex-1"><span class="block truncate font-semibold">{{ a.name }}</span><span class="block truncate text-xs text-(--ui-text-muted)">{{ a.role || a.description || t('watchW.agent.noRole') }}</span></span>
              <UIcon v-if="isSelected(a.id)" name="i-lucide-check" class="size-5 shrink-0 text-primary" />
            </button>
          </div>
        </section>
        <section v-if="pausedAgents.length" class="space-y-2">
          <h3 class="text-xs font-semibold uppercase tracking-wide text-(--ui-text-muted)">{{ t('watchW.agent.pausedGroup') }}</h3>
          <div class="grid gap-3 sm:grid-cols-2">
            <button
              v-for="a in pausedAgents" :key="a.id" type="button" :aria-pressed="isSelected(a.id)"
              class="flex min-h-24 items-center gap-3 rounded-xl border border-dashed p-4 text-start opacity-75 transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--ui-primary) hover:border-(--ui-primary) hover:opacity-100"
              :class="isSelected(a.id) ? 'border-(--ui-primary) bg-(--ui-primary)/5 opacity-100' : 'border-(--ui-border)'"
              @click="choose(a.id)"
            >
              <AgentAvatar :agent="a" size="md" class="shrink-0 grayscale" />
              <span class="min-w-0 flex-1"><span class="block truncate font-semibold">{{ a.name }}</span><span class="block truncate text-xs text-(--ui-text-muted)">{{ t('watchW.agent.paused') }}</span></span>
              <UIcon v-if="isSelected(a.id)" name="i-lucide-check" class="size-5 shrink-0 text-primary" />
            </button>
          </div>
        </section>
        <div v-if="!filteredAgents.length" class="rounded-xl border border-dashed border-(--ui-border) p-8 text-center text-sm text-(--ui-text-muted)">{{ t('watchW.agent.noResults') }}</div>
      </div>
    </template>
  </UModal>
</template>
