<script setup lang="ts">
// A box's ⚙: its title and its widget's settings (made from the widget's
// fields); lists like a project's processes load when the form opens.
import type { WatchItem } from '~/composables/useWatchLayout'
import type { WidgetField } from '~/utils/watchWidgets'

const props = defineProps<{ item: WatchItem }>()
const { t } = useLang()
const { setPane, setConfig, setSize } = useWatchLayout()
const kind = computed(() => props.item.widget)
const def = computed(() => widgetDef(kind.value))
const cfg = computed(() => widgetConfig(kind.value, props.item.config))

// the lists a field picks from, loaded from the box's project
const lists = ref<Record<string, { label: string, value: string }[]>>({})
watch(() => [props.item.projectId, kind.value] as const, async ([pid]) => {
  lists.value = {}
  for (const f of def.value.fields) {
    try {
      if (f.source === 'providers') {
        const r = await $fetch<{ providers: { id: string, name: string, enabled: boolean }[] }>('/api/providers')
        lists.value = { ...lists.value, providers: [{ label: t('watchW.provider.default'), value: '' }, ...r.providers.filter(p => p.enabled).map(p => ({ label: p.name, value: p.id }))] }
      } else if (!pid) {
        continue
      } else if (f.source === 'processes') {
        const r = await $fetch<{ processes: { id: string, name: string }[] }>(`/api/projects/${pid}/processes`)
        // a page preview may also take an address alone
        const none = kind.value === 'preview' ? [{ label: t('watchW.preview.byUrl'), value: '' }] : []
        lists.value = { ...lists.value, processes: [...none, ...r.processes.map(p => ({ label: p.name, value: p.id }))] }
      } else if (f.source === 'services') {
        const r = await $fetch<{ services: { name: string }[] }>(`/api/projects/${pid}/compose`)
        lists.value = { ...lists.value, services: r.services.map(s => ({ label: s.name, value: s.name })) }
      } else if (f.source === 'agents') {
        const r = await $fetch<{ agents: { id: string, name: string }[] }>(`/api/projects/${pid}/chat/agents`)
        lists.value = { ...lists.value, agents: [{ label: t('watchW.agent.default'), value: '' }, ...r.agents.map(a => ({ label: a.name, value: a.id }))] }
      } else if (f.source === 'monitors') {
        const r = await $fetch<{ monitors: { id: string, name: string }[] }>('/api/monitors', { query: { project: pid } })
        lists.value = { ...lists.value, monitors: [{ label: t('watchW.monitor.all'), value: '' }, ...r.monitors.map(m => ({ label: m.name, value: m.id }))] }
      }
    } catch { /* the widget itself shows the error */ }
  }
}, { immediate: true })

function optionLabel(f: WidgetField, v: string | number): string {
  switch (f.key) {
    case 'metric': return t(`watchW.stat.metric.${v}` as 'watchW.stat.metric.running')
    case 'status': return v ? t(`job.status.${v}` as 'job.status.running') : t('job.statusAll')
    case 'measure': return t(`watchW.chart.measure.${v}` as 'watchW.chart.measure.jobs')
    case 'days': return t('watchW.days', { n: v })
    case 'severity': return v ? t('watchW.incidents.errorsOnly') : t('watchW.incidents.all')
    case 'by': return t(`watchW.breakdown.by.${v}` as 'watchW.breakdown.by.project')
    case 'reload': return v ? t('watchW.board.every', { s: Number(v) < 60 ? `${v}s` : `${Number(v) / 60}m` }) : t('watchW.preview.noReload')
    case 'max': return String(v)
    default: return String(v)
  }
}
// USelect drops '' values: a sentinel stands for "all"
const ALL = '__all'
function items(f: WidgetField) {
  const list = f.source ? lists.value[f.source] ?? [] : (f.options ?? []).map(v => ({ label: optionLabel(f, v), value: v }))
  return list.map(x => ({ ...x, value: x.value === '' ? ALL : x.value }))
}
const fieldValue = (f: WidgetField) => cfg.value[f.key] === '' ? ALL : cfg.value[f.key]
const setField = (f: WidgetField, v: unknown) => setConfig(props.item.id, f.key, v === ALL ? '' : v)
</script>

<template>
  <div class="w-72 space-y-3 p-3">
    <UFormField :label="t('watchW.boxTitle')">
      <UInput :model-value="item.title ?? ''" :placeholder="t(`watchW.kind.${kind}` as 'watchW.kind.chat')" class="w-full" @update:model-value="(v) => setPane(item.id, { title: String(v) || undefined })" />
    </UFormField>
    <!-- cells wide × high, within the widget's limits -->
    <UFormField v-if="!fixedSize(def.grid)" :label="t('watchW.size.label')" :hint="t('watchW.size.range', { min: `${def.grid.minW}×${def.grid.minH}`, max: `${def.grid.maxW}×${def.grid.maxH}` })">
      <div class="flex items-center gap-2">
        <UInputNumber :model-value="item.w" :min="def.grid.minW" :max="def.grid.maxW" size="sm" class="w-24" :aria-label="t('watchW.size.w')" @update:model-value="(v) => setSize(item.id, Number(v), item.h)" />
        <span class="text-(--ui-text-dimmed)">×</span>
        <UInputNumber :model-value="item.h" :min="def.grid.minH" :max="def.grid.maxH" size="sm" class="w-24" :aria-label="t('watchW.size.h')" @update:model-value="(v) => setSize(item.id, item.w, Number(v))" />
      </div>
    </UFormField>
    <template v-for="f in def.fields" :key="f.key">
      <USwitch
        v-if="f.type === 'switch'" :model-value="!!cfg[f.key]" :label="t(`watchW.field.${f.key}` as 'watchW.field.console')"
        @update:model-value="(v) => setConfig(item.id, f.key, v)"
      />
      <UFormField v-else-if="f.type === 'text'" :label="t(`watchW.field.${f.key}` as 'watchW.field.console')">
        <UInput :model-value="String(cfg[f.key] ?? '')" class="w-full" :placeholder="t(`watchW.placeholder.${f.key}` as 'watchW.placeholder.url')" @change="(e: Event) => setConfig(item.id, f.key, (e.target as HTMLInputElement).value)" />
      </UFormField>
      <UFormField v-else :label="t(`watchW.field.${f.key}` as 'watchW.field.console')">
        <USelect
          :model-value="(fieldValue(f) as string)" :items="items(f)" class="w-full" :disabled="!!f.source && f.source !== 'providers' && !item.projectId"
          :placeholder="f.source && f.source !== 'providers' && !item.projectId ? t('watchW.pickProjectFirst') : undefined"
          @update:model-value="(v) => setField(f, v)"
        />
      </UFormField>
    </template>
  </div>
</template>
