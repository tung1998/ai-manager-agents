<script setup lang="ts">
// The machine office runs on (admins): CPU, RAM, disk as meters, cores and
// network if asked; pushed by the server, nothing polled.
const { t } = useLang()
const { config, size, narrow, tiny } = useWatchBox()
// a big box shows each core even when not asked (the room is there)
const cores = computed(() => !!config.value.cores || (size.value.w >= 400 && size.value.h >= 360))
const { stats } = useSystemStats('full')
const m = computed(() => stats.value?.machine)
const pct = (used: number, total: number) => total ? used / total * 100 : 0
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto p-3">
    <div v-if="!m" class="space-y-3"><USkeleton class="h-6 w-full" /><USkeleton class="h-6 w-full" /></div>
    <div v-else class="grid gap-3" :class="!narrow && !tiny ? 'grid-cols-2' : 'grid-cols-1'">
      <WatchMeter label="CPU" :value="m.cpu_percent" :detail="m.load?.length ? `load ${m.load.map(l => l.toFixed(2)).join(' ')}` : undefined" />
      <WatchMeter label="RAM" :value="pct(m.mem_used, m.mem_total)" :detail="`${fmtBytes(m.mem_used)} / ${fmtBytes(m.mem_total)}`" />
      <WatchMeter v-if="config.disk && !tiny" :label="t('watchW.system.disk')" :value="pct(m.disk_used, m.disk_total)" :detail="`${fmtBytes(m.disk_used)} / ${fmtBytes(m.disk_total)} · ${m.disk_path}`" />
      <div v-if="config.net && !tiny" class="grid grid-cols-2 gap-1.5">
        <WatchTile :label="t('watchW.system.rx')" :value="fmtBytes(m.net_rx)" icon="i-lucide-arrow-down" />
        <WatchTile :label="t('watchW.system.tx')" :value="fmtBytes(m.net_tx)" icon="i-lucide-arrow-up" />
      </div>
      <div v-if="cores && !tiny" class="col-span-full">
        <p class="mb-1 text-[11px] text-(--ui-text-dimmed)">{{ t('watchW.system.cores', { n: m.cores.length }) }}</p>
        <div class="flex h-10 items-end gap-0.5">
          <UTooltip v-for="(c, i) in m.cores" :key="i" :text="`#${i}: ${c.toFixed(0)}%`">
            <div class="flex h-full flex-1 items-end rounded-sm bg-(--ui-primary)/15"><div class="w-full rounded-sm bg-(--ui-primary)" :style="{ height: `${Math.max(2, c)}%` }" /></div>
          </UTooltip>
        </div>
      </div>
      <p v-if="!tiny" class="col-span-full truncate text-xs text-(--ui-text-dimmed)">{{ m.host }} · {{ m.os }} · {{ t('watchW.uptime') }} {{ fmtUptime(m.uptime_s) }}</p>
    </div>
  </div>
</template>
