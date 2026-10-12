<script setup lang="ts">
// The bot a notice goes out by: the project's own, or, switched on, one
// borrowed from another project (a project with no bot set up yet). A
// borrowed bot only announces: a reply to it does not come back here.
const props = defineProps<{ projectId: string, noneLabel: string }>()
const model = defineModel<string>({ default: '' })
const kind = defineModel<string | undefined>('kind')
const { t } = useLang()
interface Bot { id: string, name: string, kind: 'discord' | 'telegram', project: string, project_id: string }
const { data } = useLiveFetch<{ bots: Bot[] }>('/api/bots', { lazy: true })
const bots = computed(() => data.value?.bots ?? [])
const own = computed(() => bots.value.filter(b => b.project_id === props.projectId))
const others = computed(() => bots.value.filter(b => b.project_id !== props.projectId))
const picked = computed(() => bots.value.find(b => b.id === model.value))
const borrowed = computed(() => picked.value && picked.value.project_id !== props.projectId ? picked.value : undefined)
const borrowOn = ref(false)
watch(borrowed, (b) => { if (b) borrowOn.value = true }, { immediate: true })
const borrow = computed({
  get: () => borrowOn.value,
  set: (v: boolean) => {
    borrowOn.value = v
    if (!v && borrowed.value) model.value = ''
  }
})
watchEffect(() => { kind.value = picked.value?.kind })
const NONE = '__none'
const label = (b: Bot, project = false) => `${b.name} · ${b.kind === 'discord' ? 'Discord' : 'Telegram'}${project ? ' · ' + b.project : ''}`
const items = computed(() => [
  { label: props.noneLabel, value: NONE },
  ...own.value.map(b => ({ label: label(b), value: b.id })),
  ...(borrow.value ? others.value.map(b => ({ label: label(b, true), value: b.id })) : [])
])
const pick = computed({ get: () => model.value || NONE, set: (v: string) => { model.value = v === NONE ? '' : v } })
</script>

<template>
  <div class="space-y-1.5">
    <USelect v-model="pick" :items="items" class="w-full" />
    <USwitch v-if="others.length" v-model="borrow" size="xs" :label="t('notify.borrow')" />
    <p v-if="borrowed" class="flex items-start gap-1.5 text-xs text-(--ui-warning)">
      <UIcon name="i-lucide-triangle-alert" class="mt-0.5 size-3.5 shrink-0" />{{ t('notify.borrowed', { project: borrowed.project }) }}
    </p>
    <p v-else-if="data && !own.length" class="text-xs text-(--ui-text-muted)">
      {{ others.length ? t('notify.noOwn') : t('notify.noBots') }}
    </p>
  </div>
</template>
