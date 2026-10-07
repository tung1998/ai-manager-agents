<script setup lang="ts">
// An agent's fallback connections, in the order they are tried. An entry is
// a connection id, or "id|model": the same connection may come back with
// another model (the main one too, with a model of its own).
const props = defineProps<{ providers: Provider[], mainId?: string, tier?: ModelTier, disabled?: boolean }>()
const model = defineModel<string[]>({ required: true })
const { t } = useLang()

const split = (e: string) => { const [id = '', m = ''] = e.split('|'); return { id: id.trim(), model: m.trim() } }
const join = (id: string, m: string) => (m.trim() ? `${id}|${m.trim()}` : id)
const providerOf = (id: string) => props.providers.find(p => p.id === id)
const shown = computed(() => model.value.filter(e => e !== props.mainId))
const rows = computed(() => shown.value.map(split))
const options = computed(() => props.providers
  .filter(p => p.id === props.mainId || !shown.value.includes(p.id))
  .map(p => ({ label: p.enabled ? p.name : `${p.name} (${t('org.form.fallbackOff')})`, value: p.id })))
const add = (id: string) => { if (id) model.value = [...shown.value, id] }
const setModel = (i: number, m: string) => { model.value = shown.value.map((e, j) => (j === i ? join(split(e).id, m) : e)) }
function up(i: number) {
  const l = [...shown.value]
  ;[l[i - 1], l[i]] = [l[i]!, l[i - 1]!]
  model.value = l
}
const remove = (i: number) => { model.value = shown.value.filter((_, j) => j !== i) }
</script>

<template>
  <UFormField :label="t('org.form.fallbacks')" :help="t('org.form.fallbacksHelp')">
    <div class="space-y-1.5">
      <div v-for="(r, i) in rows" :key="i" class="flex items-center gap-2 rounded-md border border-(--ui-border) px-2 py-1 text-sm" :class="{ 'opacity-60': providerOf(r.id)?.enabled === false }">
        <span class="w-4 text-xs tabular-nums text-(--ui-text-muted)">{{ i + 1 }}</span>
        <span class="min-w-0 flex-1 truncate">
          {{ providerOf(r.id)?.name ?? r.id }}
          <UBadge v-if="providerOf(r.id)?.enabled === false" :label="t('org.form.fallbackOff')" size="sm" color="neutral" variant="subtle" />
        </span>
        <UInput
          :model-value="r.model" :list="`fallback-models-${i}`" size="xs" class="w-40 font-mono" :disabled="disabled"
          :placeholder="r.id === mainId ? t('org.form.fallbackModelNeeded') : (tier && providerOf(r.id)?.tier_models[tier]) || t('org.form.fallbackModel')"
          @change="(e: Event) => setModel(i, (e.target as HTMLInputElement).value)"
        />
        <datalist :id="`fallback-models-${i}`"><option v-for="m in providerOf(r.id)?.models ?? []" :key="m" :value="m" /></datalist>
        <template v-if="!disabled">
          <UButton v-if="i > 0" size="xs" color="neutral" variant="ghost" icon="i-lucide-arrow-up" :aria-label="t('org.form.fallbackUp')" @click="up(i)" />
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-x" :aria-label="t('org.form.fallbackRemove')" @click="remove(i)" />
        </template>
      </div>
      <USelect
        v-if="!disabled && options.length" :model-value="undefined" :items="options" :placeholder="t('org.form.fallbackAdd')"
        icon="i-lucide-plus" size="sm" class="w-full sm:w-72" @update:model-value="(v: string) => add(v)"
      />
    </div>
  </UFormField>
</template>
