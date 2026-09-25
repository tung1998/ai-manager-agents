<script setup lang="ts">
import type { Permissions } from '~/composables/useOffice'
// An agent's permissions: pick a package (a preset), then switch single
// capabilities on or off and narrow the commands it may run by itself.
const perms = defineModel<Permissions>({ required: true })
const props = defineProps<{ projectId?: string, disabled?: boolean }>()

const level = computed(() => agentLevel(perms.value))
const caps = computed(() => agentCaps(perms.value))
const custom = computed(() => !!perms.value.caps)
const base = computed(() => perms.value.level ?? level.value)

function pickPreset(l: PermLevel) {
  perms.value.level = l
  perms.value.caps = null
  perms.value.read_only = l === 'read'
}

// a pick that equals a package's preset goes back to being that package
function setCaps(next: string[]) {
  const same = permLevels.find(p => {
    const pre = presetCaps(p.level)
    return pre.length === next.length && pre.every(c => next.includes(c))
  })
  if (same) return pickPreset(same.level)
  perms.value.caps = permCaps.map(c => c.id).filter(id => next.includes(id))
  perms.value.read_only = agentLevel(perms.value) === 'read'
}
function toggle(id: string, on: boolean) {
  let next = caps.value.filter(c => c !== id)
  if (on) next.push(id)
  if (on && id !== 'propose' && !next.includes('propose')) next.push('propose') // acting alone implies proposing
  if (!on && id === 'propose') next = [] // nothing is done alone without proposing
  setCaps(next)
}

// ---- commands the agent may run by itself (within the project's list) ----
const { data: pol } = useFetch<{ policy: { commands: string[] }, packs: CommandPack[] }>(
  () => `/api/projects/${props.projectId}/policy`, { lazy: true, immediate: !!props.projectId })
const projectCmds = computed(() => pol.value?.policy.commands ?? [])
const tree = computed(() => {
  const seen = new Set<string>()
  const groups: { id: string, label: string, icon?: string, commands: string[] }[] = []
  for (const p of pol.value?.packs ?? []) {
    const cmds = p.commands.filter(c => projectCmds.value.includes(c) && !seen.has(c))
    cmds.forEach(c => seen.add(c))
    if (cmds.length) groups.push({ ...p, commands: cmds })
  }
  const rest = projectCmds.value.filter(c => !seen.has(c))
  if (rest.length) groups.push({ id: 'other', label: 'Khác', icon: 'i-lucide-terminal', commands: rest })
  return groups
})
const allCmds = computed(() => perms.value.commands == null)
const picked = computed(() => perms.value.commands ?? projectCmds.value)
function setAll(on: boolean) { perms.value.commands = on ? null : [...projectCmds.value] }
function toggleCmd(c: string, on: boolean) {
  const next = picked.value.filter(x => x !== c)
  if (on) next.push(c)
  perms.value.commands = next
}
function packState(cmds: string[]): boolean | 'indeterminate' {
  const n = cmds.filter(c => picked.value.includes(c)).length
  return n === 0 ? false : n === cmds.length ? true : 'indeterminate'
}
function togglePack(cmds: string[]) {
  const on = packState(cmds) !== true
  const next = picked.value.filter(c => !cmds.includes(c))
  perms.value.commands = on ? [...next, ...cmds] : next
}
</script>

<template>
  <div class="space-y-3">
    <!-- packages -->
    <div class="flex flex-wrap gap-1 rounded-lg bg-(--ui-bg-elevated) p-1">
      <button
        v-for="p in permLevels" :key="p.level" type="button" :disabled="disabled"
        class="flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition"
        :class="!custom && level === p.level ? 'bg-(--ui-bg) text-(--ui-text-highlighted) shadow-sm' : 'text-(--ui-text-muted) hover:text-(--ui-text)'"
        @click="pickPreset(p.level)"
      >
        <UIcon :name="p.icon" class="size-3.5" :class="!custom && level === p.level ? 'text-primary' : ''" />{{ p.label }}
      </button>
      <span v-if="custom" class="flex items-center gap-1.5 rounded-md bg-(--ui-bg) px-2.5 py-1.5 text-xs font-medium text-(--ui-text-highlighted) shadow-sm">
        <UIcon name="i-lucide-sliders-horizontal" class="size-3.5 text-primary" />Tùy chỉnh
      </span>
    </div>
    <p class="text-xs text-(--ui-text-muted)">
      <template v-if="custom">
        Tùy chỉnh từ gói {{ permOf(base).label }}.
        <button v-if="!disabled" type="button" class="text-primary underline-offset-2 hover:underline" @click="pickPreset(base)">Về gói {{ permOf(base).label }}</button>
      </template>
      <template v-else>{{ permOf(level).description }}. Gói cao gồm cả gói thấp; bật/tắt từng quyền bên dưới nếu cần.</template>
    </p>

    <!-- single capabilities, by group -->
    <div class="grid gap-2 sm:grid-cols-2">
      <div v-for="g in permGroups" :key="g.id" class="rounded-lg border border-(--ui-border)">
        <p class="flex items-center gap-1.5 border-b border-(--ui-border) px-3 py-1.5 text-xs font-medium text-(--ui-text-muted)">
          <UIcon :name="g.icon" class="size-3.5" />{{ g.label }}
        </p>
        <div class="divide-y divide-(--ui-border)">
          <label
            v-for="c in permCaps.filter(x => x.group === g.id)" :key="c.id"
            class="flex items-start gap-2.5 px-3 py-2" :class="disabled ? '' : 'cursor-pointer hover:bg-(--ui-bg-elevated)/50'"
          >
            <USwitch size="sm" class="mt-0.5" :model-value="caps.includes(c.id)" :disabled="disabled" @update:model-value="(v: boolean) => toggle(c.id, v)" />
            <span class="min-w-0 flex-1">
              <span class="text-sm">{{ c.label }}</span>
              <span class="block text-xs text-(--ui-text-muted)">{{ c.description }}</span>
            </span>
            <UBadge size="sm" color="neutral" variant="outline" :label="permOf(c.min).label" class="shrink-0" :title="`Có trong gói ${permOf(c.min).label} trở lên`" />
          </label>
          <div v-if="g.id === 'git'" class="flex items-start gap-2.5 px-3 py-2 text-(--ui-text-muted)">
            <UIcon name="i-lucide-lock" class="mt-0.5 size-4 shrink-0" />
            <span class="text-xs">Push lên remote luôn cần bạn duyệt, không bao giờ force.</span>
          </div>
          <!-- commands it may run by itself -->
          <div v-if="g.id === 'commands' && caps.includes('commands.run')" class="space-y-2 px-3 py-2">
            <p v-if="!projectId" class="text-xs text-(--ui-text-muted)">Lệnh cụ thể chọn khi mô hình gắn với project.</p>
            <p v-else-if="!projectCmds.length" class="text-xs text-(--ui-text-muted)">Project chưa bật lệnh nào (Cấu hình → Quyền → Lệnh).</p>
            <template v-else>
              <label class="flex cursor-pointer items-center gap-2 text-xs">
                <UCheckbox :model-value="allCmds" :disabled="disabled" @update:model-value="(v: boolean | 'indeterminate') => setAll(v === true)" />
                Mọi lệnh project cho phép <span class="text-(--ui-text-muted)">({{ projectCmds.length }})</span>
              </label>
              <div v-if="!allCmds" class="space-y-1.5">
                <details v-for="p in tree" :key="p.id" class="rounded-md bg-(--ui-bg-elevated)/60 px-2 py-1" open>
                  <summary class="flex cursor-pointer list-none items-center gap-2 text-xs font-medium">
                    <UCheckbox :model-value="packState(p.commands)" :disabled="disabled" @click.stop @update:model-value="togglePack(p.commands)" />
                    <UIcon v-if="p.icon" :name="p.icon" class="size-3.5" />{{ p.label }}
                    <span class="ms-auto font-normal text-(--ui-text-muted)">{{ p.commands.filter(c => picked.includes(c)).length }}/{{ p.commands.length }}</span>
                  </summary>
                  <label v-for="c in p.commands" :key="c" class="flex cursor-pointer items-center gap-2 py-0.5 ps-6 text-xs">
                    <UCheckbox :model-value="picked.includes(c)" :disabled="disabled" @update:model-value="(v: boolean | 'indeterminate') => toggleCmd(c, v === true)" />
                    <code class="truncate">{{ c }}</code>
                  </label>
                </details>
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
