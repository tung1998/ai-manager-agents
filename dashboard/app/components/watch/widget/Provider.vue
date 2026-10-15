<script setup lang="ts">
// AI connections as in the header, shown by the box's room: a badge (the
// ring), a strip (ring, name, a bar per window with its reset countdown), a
// card (a large ring, the 5 hours in its middle, the week under it).
import type { ProviderRingData } from '~/composables/useProviderRings'

const { t } = useLang()
const { config, shape, cells } = useWatchBox()
const { rings, providers, loaded, refresh } = useProviderRings()
onMounted(() => refresh())
useWatchPoll(() => refresh(), 60) // the windows come from the vendors: no event
const now = useNow(30000)

const ring = computed(() => {
  const id = String(config.value.providerId || '') || providers.value.find(p => p.is_default)?.id
  return rings.value.find(r => r.id === id) ?? null
})
const left = (iso: string) => iso ? fmtLeft(Date.parse(iso) - now.value) : ''
const winLabel = (k: string) => t(`limits.window.${k}` as 'limits.window.five_hour')
const tone = (p: number) => p >= 90 ? 'bg-(--ui-error)' : p >= 70 ? 'bg-(--ui-warning)' : 'bg-(--ui-primary)'
const five = (r: ProviderRingData) => r.windows.find(w => w.key === 'five_hour')
const week = (r: ProviderRingData) => r.windows.find(w => w.key === 'seven_day')
</script>

<template>
  <div v-if="!loaded" class="grid h-full place-items-center"><USkeleton class="size-11 rounded-full" /></div>
  <WatchEmpty v-else-if="!ring" icon="i-lucide-plug-zap" :text="t('watchW.provider.none')" />

  <!-- badge: the ring, the name under it -->
  <NuxtLink
    v-else-if="shape === 'badge' || shape === 'column'" to="/providers" :title="ring.title"
    class="flex h-full min-h-0 flex-col items-center justify-center gap-0.5 overflow-hidden p-1 transition hover:bg-(--ui-bg-elevated)/50"
  >
    <ProviderRing :ring="ring" logo-class="size-4 text-[10px]" class="size-11 shrink-0" />
    <span class="w-full truncate text-center text-[11px] leading-tight font-medium">{{ ring.name }}</span>
    <span v-if="ring.error || ring.windows.length" class="w-full truncate text-center text-[10px] leading-none tabular-nums" :class="ring.error ? 'text-(--ui-error)' : 'text-(--ui-text-muted)'">
      {{ ring.error ? t('prov.statusError') : ring.windows.map(w => `${w.pct}%`).join(' · ') }}
    </span>
  </NuxtLink>

  <!-- strip: ring, name, a bar per window with the time until it resets -->
  <NuxtLink v-else-if="shape === 'strip'" to="/providers" :title="ring.title" class="flex h-full min-h-0 items-center gap-2.5 overflow-hidden px-2.5 transition hover:bg-(--ui-bg-elevated)/50">
    <ProviderRing :ring="ring" logo-class="size-4 text-[10px]" class="size-11 shrink-0" />
    <span class="min-w-0 flex-1 space-y-1">
      <span class="block truncate text-xs font-medium">{{ ring.name }}</span>
      <span v-if="ring.error" class="block truncate text-[11px] text-(--ui-error)">{{ t('prov.statusError') }}</span>
      <span v-for="w in ring.windows" v-else :key="w.key" class="flex items-center gap-1.5 text-[10px] text-(--ui-text-muted) tabular-nums">
        <span class="h-1 min-w-0 flex-1 overflow-hidden rounded-full bg-(--ui-bg-accented)"><span class="block h-full rounded-full" :class="tone(w.pct)" :style="{ width: `${Math.min(100, w.pct)}%` }" /></span>
        <span class="shrink-0">{{ w.pct }}%</span>
        <span v-if="cells.w > 2" class="shrink-0 text-(--ui-text-dimmed)">↻ {{ left(w.resetsAt) }}</span>
      </span>
    </span>
  </NuxtLink>

  <!-- card: a large ring, the 5 hours inside, the week and the resets under it -->
  <NuxtLink v-else to="/providers" :title="ring.title" class="flex h-full min-h-0 flex-col items-center justify-center gap-1.5 overflow-hidden p-2 transition hover:bg-(--ui-bg-elevated)/50">
    <span class="relative grid place-items-center">
      <ProviderRing :ring="ring" logo-class="size-5 text-xs" class="size-24" />
      <span v-if="five(ring)" class="absolute bottom-3.5 text-[11px] font-semibold tabular-nums">{{ five(ring)!.pct }}%</span>
    </span>
    <span class="flex max-w-full items-center gap-1 truncate text-sm font-medium">
      <UIcon v-if="ring.isDefault" name="i-lucide-star" class="size-3 shrink-0 text-(--ui-warning)" />{{ ring.name }}
    </span>
    <span v-if="ring.error" class="truncate text-xs text-(--ui-error)">{{ t('prov.statusError') }}</span>
    <span v-else-if="ring.windows.length" class="flex flex-wrap justify-center gap-x-2 text-[11px] text-(--ui-text-muted) tabular-nums">
      <span v-if="five(ring)">{{ winLabel('five_hour') }} ↻ {{ left(five(ring)!.resetsAt) }}</span>
      <span v-if="week(ring)">{{ winLabel('seven_day') }} {{ week(ring)!.pct }}%</span>
    </span>
  </NuxtLink>
</template>
