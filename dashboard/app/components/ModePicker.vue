<script setup lang="ts">
// Permission mode for a chat or a task: a ceiling on what its agents may do on
// their own. Members can only read or ask first; the project caps the rest.
const props = defineProps<{ projectId: string }>()
const mode = defineModel<PermLevel>({ default: 'propose' })
const { isAdmin } = useAuth()
const { t } = useLang()
const { data } = useFetch<{ policy: { max_level: PermLevel } }>(() => `/api/projects/${props.projectId}/policy`, { lazy: true })
const cap = computed(() => data.value?.policy.max_level ?? 'propose')

const items = computed(() => [permLevels.map((p) => {
  const overCap = permRank(p.level) > permRank(cap.value)
  const adminOnly = !isAdmin.value && permRank(p.level) > permRank('propose')
  return {
    label: p.label,
    description: overCap ? t('mode.overCap', { label: permOf(cap.value).label }) : adminOnly ? t('mode.adminOnly') : p.description,
    icon: p.icon,
    disabled: overCap || adminOnly,
    active: mode.value === p.level,
    onSelect: () => { mode.value = p.level }
  }
})])
</script>

<template>
  <UDropdownMenu :items="items" :content="{ align: 'end', side: 'top' }" :ui="{ content: 'w-80' }">
    <UButton
      size="xs" :color="permRank(mode) >= 2 ? 'warning' : 'neutral'" variant="soft"
      :icon="permOf(mode).icon" :label="permOf(mode).label" trailing-icon="i-lucide-chevron-up"
      :title="t('mode.title', { description: permOf(mode).description })"
    />
  </UDropdownMenu>
</template>
