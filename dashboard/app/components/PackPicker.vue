<script setup lang="ts">
// Pick a starter pack (ADR-099): the agents a project begins with and the
// workflows installed for it. allowAi adds "let AI propose" ('__ai').
const props = defineProps<{ allowAi?: boolean }>()
const selected = defineModel<string>({ default: '' })
const { t } = useLang()

const { data } = useLiveFetch<{ packs: Pack[] }>('/api/packs', { lazy: true })
const packs = computed(() => data.value?.packs ?? [])
// nothing picked yet: the first pack
watch(packs, (l) => { if (!selected.value && !props.allowAi && l[0]) selected.value = l[0].key }, { immediate: true })
const packIcon = (key: string) => ({ solo: 'i-lucide-user', team: 'i-lucide-users', council: 'i-lucide-landmark' } as Record<string, string>)[key] ?? 'i-lucide-package'
</script>

<template>
  <div class="grid gap-2">
    <button
      v-if="allowAi" type="button"
      class="flex items-start gap-3 rounded-lg border p-3 text-left transition"
      :class="selected === '__ai' ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
      @click="selected = '__ai'"
    >
      <UIcon name="i-lucide-sparkles" class="mt-0.5 size-5 shrink-0 text-primary" />
      <div class="min-w-0">
        <p class="font-medium">{{ t('team.pack.aiSuggest') }} <span class="text-xs font-normal text-(--ui-text-muted)">{{ t('team.pack.aiRecommended') }}</span></p>
        <p class="text-sm text-(--ui-text-muted)">{{ t('team.pack.aiDesc') }}</p>
      </div>
    </button>
    <LoadingRows v-if="!data" :n="3" />
    <button
      v-for="p in packs" :key="p.key" type="button"
      class="flex items-start gap-3 rounded-lg border p-3 text-left transition"
      :class="selected === p.key ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
      @click="selected = p.key"
    >
      <UIcon :name="packIcon(p.key)" class="mt-0.5 size-5 shrink-0 text-primary" />
      <div class="min-w-0 space-y-0.5">
        <p class="font-medium">
          {{ p.name }}
          <span class="text-xs font-normal text-(--ui-text-muted)">
            · {{ t('team.agentCount', { n: p.agents.length }) }}<template v-if="p.workflows.length"> · {{ t('team.pack.workflowCount', { n: p.workflows.length }) }}</template>
          </span>
        </p>
        <p class="line-clamp-2 text-sm text-(--ui-text-muted)">{{ p.description }}</p>
        <p class="truncate text-xs text-(--ui-text-dimmed)" :title="p.agents.map(a => a.name).join(', ')">{{ p.agents.map(a => a.name).join(', ') }}</p>
      </div>
    </button>
  </div>
</template>
