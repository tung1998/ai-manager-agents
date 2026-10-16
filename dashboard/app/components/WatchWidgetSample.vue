<script setup lang="ts">
import type { WidgetKind } from '~/utils/watchWidgets'

defineProps<{ kind: WidgetKind }>()

const bars = [32, 55, 39, 76, 48, 88, 62, 100, 69, 82, 57, 93]
const rows = [0, 1, 2]
</script>

<template>
  <div class="h-32 shrink-0 border-b border-(--ui-border) bg-(--ui-bg-elevated)/35 p-2.5" aria-hidden="true">
    <div class="relative h-full overflow-hidden rounded-md border border-(--ui-border) bg-(--ui-bg) p-2.5 text-[9px] text-(--ui-text-muted)">
      <template v-if="kind === 'chat'">
        <div class="mb-2 flex items-center gap-1.5 border-b border-(--ui-border) pb-1.5"><span class="size-2 rounded-full bg-(--ui-primary)" /><span class="h-1.5 w-16 rounded bg-(--ui-text-muted)/35" /><span class="ms-auto h-1.5 w-7 rounded bg-(--ui-border)" /></div>
        <div class="space-y-1.5"><div class="flex gap-1.5"><span class="size-3 shrink-0 rounded-full bg-(--ui-primary)/30" /><span class="h-5 w-3/5 rounded-md bg-(--ui-bg-elevated)" /></div><div class="ms-auto h-5 w-2/3 rounded-md bg-(--ui-primary)/15" /><div class="flex gap-1.5"><span class="size-3 shrink-0 rounded-full bg-(--ui-success)/30" /><span class="h-5 w-1/2 rounded-md bg-(--ui-bg-elevated)" /></div></div>
      </template>
      <template v-else-if="['agent', 'jobs', 'incidents', 'approvals', 'automations', 'burnwork'].includes(kind)">
        <div v-if="kind === 'agent'" class="mb-2 flex items-center gap-2"><span class="relative size-7 rounded-full bg-(--ui-primary)/15 ring-2 ring-(--ui-primary)/50"><span class="absolute bottom-0 end-0 size-2 rounded-full bg-(--ui-success)" /></span><span class="flex-1"><span class="block h-1.5 w-20 rounded bg-(--ui-text-muted)/45" /><span class="mt-1.5 block h-1 w-28 rounded bg-(--ui-border)" /></span><span class="h-4 w-9 rounded bg-(--ui-bg-elevated)" /></div>
        <div v-for="row in rows" :key="row" class="flex items-center gap-2 border-t border-(--ui-border)/70 py-1.5">
          <span class="size-1.5 shrink-0 rounded-full" :class="kind === 'incidents' && row === 0 ? 'bg-(--ui-error)' : kind === 'approvals' ? 'bg-(--ui-warning)' : 'bg-(--ui-success)'" />
          <span class="h-1.5 rounded bg-(--ui-text-muted)/30" :class="row === 0 ? 'w-24' : 'w-16'" />
          <span class="ms-auto h-1.5 w-8 rounded bg-(--ui-border)" />
          <span v-if="kind === 'approvals'" class="size-3 rounded border border-(--ui-primary)/50" />
        </div>
      </template>
      <template v-else-if="kind === 'stat'">
        <div class="h-1.5 w-20 rounded bg-(--ui-text-muted)/30" /><div class="mt-2 flex items-center justify-between"><span class="text-3xl font-semibold leading-none text-(--ui-text-highlighted)">12</span><div class="flex h-8 items-end gap-0.5"><span v-for="(height, i) in bars.slice(0, 7)" :key="i" class="w-1.5 rounded-t-sm bg-(--ui-primary)/55" :style="{ height: `${height * 0.28}px` }" /></div></div><div class="mt-2 h-1 w-24 rounded bg-(--ui-border)" />
      </template>
      <template v-else-if="kind === 'chart' || kind === 'breakdown'">
        <div class="mb-2 flex items-center justify-between"><span class="h-1.5 w-20 rounded bg-(--ui-text-muted)/35" /><span class="h-3 w-10 rounded bg-(--ui-bg-elevated)" /></div>
        <div v-if="kind === 'chart'" class="flex h-16 items-end gap-1 border-b border-(--ui-border) px-1"><span v-for="(height, i) in bars" :key="i" class="flex-1 rounded-t-sm" :class="i === 7 ? 'bg-(--ui-primary)' : 'bg-(--ui-primary)/40'" :style="{ height: `${height}%` }" /></div>
        <div v-else class="space-y-2.5 pt-1"><div v-for="(width, i) in [82, 57, 38]" :key="i" class="flex items-center gap-2"><span class="h-1.5 w-12 rounded bg-(--ui-text-muted)/30" /><span class="h-2 flex-1 rounded-full bg-(--ui-bg-elevated)"><span class="block h-full rounded-full bg-(--ui-primary)/60" :style="{ width: `${width}%` }" /></span><span class="h-1.5 w-7 rounded bg-(--ui-border)" /></div></div>
      </template>
      <template v-else-if="kind === 'process'">
        <div class="mb-2 flex items-center gap-1.5"><span class="size-1.5 rounded-full bg-(--ui-success)" /><span class="h-1.5 w-20 rounded bg-(--ui-text-muted)/35" /><span class="ms-auto rounded bg-(--ui-success)/10 px-1.5 py-0.5 text-(--ui-success)">RUNNING</span></div>
        <div class="mb-2 grid grid-cols-2 gap-1.5"><span v-for="n in 2" :key="n" class="rounded border border-(--ui-border) p-1.5"><i class="mb-1 block h-1 w-8 rounded bg-(--ui-border)" /><i class="block h-1.5 w-12 rounded bg-(--ui-primary)/40" /></span></div>
        <div class="space-y-1 rounded bg-black/20 p-1.5 font-mono"><span v-for="n in 3" :key="n" class="block h-1 w-3/4 rounded bg-(--ui-success)/35" /></div>
      </template>
      <template v-else-if="kind === 'container' || kind === 'consoles'">
        <div v-if="kind === 'container'" class="mb-1.5 flex items-center gap-1.5"><UIcon name="i-lucide-container" class="size-3 text-(--ui-primary)" /><span class="h-1.5 w-20 rounded bg-(--ui-text-muted)/35" /><span class="ms-auto size-1.5 rounded-full bg-(--ui-success)" /></div>
        <div :class="kind === 'consoles' ? 'grid h-full grid-cols-2 grid-rows-2 gap-1' : 'space-y-1.5'">
          <div v-for="n in (kind === 'consoles' ? 4 : 3)" :key="n" class="rounded border border-(--ui-border) bg-black/10 p-1.5"><div v-if="kind === 'consoles'" class="mb-1 h-1 w-8 rounded bg-(--ui-primary)/45" /><span v-for="line in (kind === 'consoles' ? 2 : 1)" :key="line" class="mb-1 block h-1 rounded bg-(--ui-text-muted)/25" :class="line === 1 ? 'w-4/5' : 'w-3/5'" /></div>
        </div>
      </template>
      <template v-else-if="kind === 'monitor' || kind === 'lights'">
        <div v-if="kind === 'monitor'" class="flex h-full items-center gap-3"><span class="grid size-9 shrink-0 place-items-center rounded-full border-4 border-(--ui-success)/50 text-(--ui-success)"><UIcon name="i-lucide-heart-pulse" class="size-4" /></span><div class="min-w-0 flex-1"><div class="mb-2 flex justify-between"><span class="h-1.5 w-16 rounded bg-(--ui-text-muted)/35" /><span class="h-1.5 w-8 rounded bg-(--ui-success)/50" /></div><div class="flex h-9 items-end gap-0.5"><span v-for="(height, i) in bars" :key="i" class="flex-1 rounded-t-sm bg-(--ui-success)/45" :style="{ height: `${height}%` }" /></div></div></div>
        <div v-else class="grid h-full grid-cols-2 gap-2"><div v-for="n in 4" :key="n" class="flex items-center gap-2 rounded border border-(--ui-border) px-2"><span class="size-2 rounded-full" :class="n === 4 ? 'bg-(--ui-warning)' : 'bg-(--ui-success)'" /><span class="h-1.5 w-12 rounded bg-(--ui-text-muted)/35" /></div></div>
      </template>
      <template v-else-if="kind === 'system'">
        <div class="mb-2 flex items-center justify-between"><span class="h-1.5 w-16 rounded bg-(--ui-text-muted)/35" /><span class="h-1.5 w-10 rounded bg-(--ui-border)" /></div>
        <div class="grid grid-cols-2 gap-x-3 gap-y-2"><div v-for="(width, i) in [62, 42, 78, 26]" :key="i"><div class="mb-1 flex justify-between"><span class="h-1 w-8 rounded bg-(--ui-border)" /><span class="h-1 w-5 rounded bg-(--ui-text-muted)/30" /></div><span class="block h-1.5 rounded-full bg-(--ui-bg-elevated)"><i class="block h-full rounded-full bg-(--ui-primary)/60" :style="{ width: `${width}%` }" /></span></div></div>
      </template>
      <template v-else-if="kind === 'budget' || kind === 'limits' || kind === 'provider'">
        <div v-if="kind === 'provider'" class="flex h-full items-center gap-3"><span class="grid size-12 shrink-0 place-items-center rounded-full" style="background: conic-gradient(var(--ui-primary) 68%, var(--ui-bg-elevated) 0)"><span class="grid size-9 place-items-center rounded-full bg-(--ui-bg) text-[8px] text-(--ui-text)">68%</span></span><div class="min-w-0 flex-1 space-y-2"><div class="h-1.5 w-14 rounded bg-(--ui-text-muted)/35" /><span class="block h-1.5 rounded-full bg-(--ui-bg-elevated)"><i class="block h-full w-2/3 rounded-full bg-(--ui-primary)/60" /></span><span class="block h-1.5 w-4/5 rounded-full bg-(--ui-border)" /></div></div>
        <div v-else class="flex h-full flex-col justify-center"><div class="mb-2 flex items-end justify-between"><span class="text-2xl font-semibold leading-none text-(--ui-text-highlighted)">{{ kind === 'budget' ? '$4.20' : '5h' }}</span><span class="h-1.5 w-12 rounded bg-(--ui-text-muted)/30" /></div><div class="mb-2 h-2 rounded-full bg-(--ui-bg-elevated)"><span class="block h-full w-2/3 rounded-full" :class="kind === 'budget' ? 'bg-(--ui-primary)' : 'bg-(--ui-warning)'" /></div><div class="h-1 w-24 rounded bg-(--ui-border)" /></div>
      </template>
      <template v-else-if="kind === 'note'">
        <div class="mx-auto flex h-full max-w-52 rotate-[-1deg] flex-col rounded bg-(--ui-warning)/10 p-3 shadow-sm"><span class="mb-2 h-1.5 w-16 rounded bg-(--ui-warning)/55" /><span v-for="n in 4" :key="n" class="mb-2 h-1 rounded bg-(--ui-text-muted)/30" :class="n === 4 ? 'w-2/3' : 'w-full'" /></div>
      </template>
      <template v-else-if="kind === 'git'">
        <div class="mb-2 flex items-center gap-1.5 rounded bg-(--ui-bg-elevated) px-2 py-1"><UIcon name="i-lucide-git-branch" class="size-3 text-(--ui-primary)" /><span class="h-1.5 w-16 rounded bg-(--ui-text-muted)/35" /><span class="ms-auto h-1.5 w-8 rounded bg-(--ui-success)/40" /></div>
        <div v-for="n in 3" :key="n" class="flex items-center gap-2 border-b border-(--ui-border)/70 py-1.5"><UIcon name="i-lucide-file-code-2" class="size-3 text-(--ui-text-dimmed)" /><span class="h-1.5 w-24 rounded bg-(--ui-text-muted)/30" /><span class="ms-auto text-(--ui-success)">+{{ n * 3 }}</span></div>
      </template>
      <template v-else-if="kind === 'controls'">
        <div v-for="n in 3" :key="n" class="flex items-center gap-2 border-b border-(--ui-border)/70 py-2"><UIcon name="i-lucide-square-terminal" class="size-3 text-(--ui-text-dimmed)" /><span class="h-1.5 w-20 rounded bg-(--ui-text-muted)/30" /><span class="ms-auto h-3.5 w-7 rounded-full bg-(--ui-success)/30 p-0.5"><i class="ms-auto block size-2.5 rounded-full bg-(--ui-success)" /></span></div>
      </template>
      <template v-else-if="kind === 'preview'">
        <div class="mb-2 flex items-center gap-1 border-b border-(--ui-border) pb-1.5"><span v-for="n in 3" :key="n" class="size-1.5 rounded-full bg-(--ui-border)" /><span class="ms-1 h-2 flex-1 rounded-full bg-(--ui-bg-elevated)" /></div>
        <div class="mx-auto max-w-44 text-center"><div class="mx-auto mb-2 h-2 w-20 rounded bg-(--ui-primary)/35" /><div class="mx-auto mb-2 h-1 w-32 rounded bg-(--ui-text-muted)/20" /><div class="mx-auto h-1 w-24 rounded bg-(--ui-text-muted)/20" /><div class="mx-auto mt-3 h-7 w-20 rounded-md bg-(--ui-primary)/20" /></div>
      </template>
      <template v-else>
        <div class="grid h-full place-items-center"><UIcon :name="widgetDef(kind).icon" class="size-8 text-(--ui-primary)/60" /></div>
      </template>
    </div>
  </div>
</template>
