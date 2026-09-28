<script setup lang="ts">
// A project's limits on its agents: the highest package any agent may use
// here, what they may run or restart on their own, and files no diff may touch.
interface Policy { allowed_commands: string[], commands: string[], packs: CommandPack[], allowed_containers: string[], deny_paths: string[], isolate_claude: boolean, edit_mode: 'worktree' | 'direct', worktree_links: string[] }
interface Proc { id: string, name: string, command: string, kind: 'service' | 'job' }

const props = defineProps<{ projectId: string }>()
const toast = useToast()
const { isAdmin } = useAuth()
const { t } = useLang()

const { data, refresh } = await useFetch<{ policy: Policy, packs: CommandPack[] }>(() => `/api/projects/${props.projectId}/policy`)
const { data: procData } = useFetch<{ processes: Proc[] }>(() => `/api/projects/${props.projectId}/processes`, { lazy: true })
const { data: composeData } = useFetch<{ services: { name: string }[] }>(() => `/api/projects/${props.projectId}/compose`, { lazy: true })
const procs = computed(() => procData.value?.processes ?? [])
const services = computed(() => composeData.value?.services ?? [])

const form = reactive({ allowed_commands: [] as string[], commands: [] as string[], packs: [] as CommandPack[], allowed_containers: [] as string[], deny: [] as string[], isolate_claude: false, edit_mode: 'worktree' as 'worktree' | 'direct', links: '' })
watch(data, (d) => {
  if (!d) return
  form.allowed_commands = [...d.policy.allowed_commands]
  form.commands = [...d.policy.commands]
  form.packs = JSON.parse(JSON.stringify(d.policy.packs))
  form.allowed_containers = [...d.policy.allowed_containers]
  form.deny = [...d.policy.deny_paths]
  form.isolate_claude = !!d.policy.isolate_claude
  form.edit_mode = d.policy.edit_mode === 'direct' ? 'direct' : 'worktree'
  form.links = (d.policy.worktree_links ?? []).join('\n')
}, { immediate: true })

function toggle(list: string[], v: string) {
  const i = list.indexOf(v)
  if (i >= 0) list.splice(i, 1)
  else list.push(v)
}
const editModes = computed(() => [
  { value: 'worktree' as const, icon: 'i-lucide-git-branch', label: t('policy.editWorktree'), desc: t('policy.editWorktreeDesc') },
  { value: 'direct' as const, icon: 'i-lucide-pencil', label: t('policy.editDirect'), desc: t('policy.editDirectDesc') }
])
const newDeny = ref('')
function addDeny() {
  const v = newDeny.value.trim()
  if (v && !form.deny.includes(v)) form.deny.push(v)
  newDeny.value = ''
}
const advanced = ref(false)
const saving = ref(false)
async function save() {
  saving.value = true
  try {
    await $fetch(`/api/projects/${props.projectId}/policy`, {
      method: 'PUT',
      body: { allowed_commands: form.allowed_commands, commands: form.commands, packs: form.packs.filter(p => p.label.trim()), allowed_containers: form.allowed_containers, deny_paths: form.deny, isolate_claude: form.isolate_claude, edit_mode: form.edit_mode, worktree_links: form.links.split('\n').map(s => s.trim()).filter(Boolean) }
    })
    toast.add({ title: t('policy.saved'), color: 'success' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    saving.value = false
  }
}
// ---- commands: built-in/detected packs + the project's own packs ----
const packs = computed<CommandPack[]>(() => [...(data.value?.packs ?? []).filter(p => !p.custom), ...form.packs.map(p => ({ ...p, custom: true }))])
const enabledIn = (p: CommandPack) => p.commands.filter(c => form.commands.includes(c)).length
function packState(p: CommandPack): boolean | 'indeterminate' {
  const n = enabledIn(p)
  return n === 0 ? false : n === p.commands.length ? true : 'indeterminate'
}
function togglePack(p: CommandPack) {
  const on = packState(p) !== true
  form.commands = form.commands.filter(c => !p.commands.includes(c))
  if (on) form.commands.push(...p.commands)
}
function toggleCmd(c: string, on: boolean) {
  form.commands = form.commands.filter(x => x !== c)
  if (on) form.commands.push(c)
}
const newCmd = reactive<Record<string, string>>({})
function addCmd(p: CommandPack) {
  const pack = form.packs.find(x => x.id === p.id)
  const c = (newCmd[p.id] ?? '').trim().replace(/\s+/g, ' ')
  if (!pack || !c) return
  if (/[;&|<>$`\\]/.test(c)) {
    toast.add({ title: t('policy.shellWarning'), color: 'warning' })
    return
  }
  if (!pack.commands.includes(c)) pack.commands.push(c)
  if (!form.commands.includes(c)) form.commands.push(c)
  newCmd[p.id] = ''
}
function removeCmd(p: CommandPack, c: string) {
  const pack = form.packs.find(x => x.id === p.id)
  if (pack) pack.commands = pack.commands.filter(x => x !== c)
  form.commands = form.commands.filter(x => x !== c)
}
function addPack() {
  form.packs.push({ id: `custom-new-${Date.now()}`, label: t('policy.newPackLabel', { n: form.packs.length + 1 }), commands: [] })
}
function removePack(p: CommandPack) {
  form.packs = form.packs.filter(x => x.id !== p.id)
  form.commands = form.commands.filter(c => !p.commands.includes(c) || packs.value.some(o => o.id !== p.id && o.commands.includes(c)))
}

const needs = (p: Proc) => p.kind === 'job' ? t('policy.needsJob') : t('policy.needsService')
</script>

<template>
  <div class="max-w-4xl space-y-3">
    <!-- where code changes happen -->
    <UCard :ui="{ body: 'space-y-3 sm:p-4' }">
      <p class="text-sm font-medium">{{ t('policy.editMode') }}</p>
      <div class="grid gap-2 sm:grid-cols-2">
        <UTooltip v-for="m in editModes" :key="m.value" :text="m.desc">
          <button
            type="button" :disabled="!isAdmin"
            class="flex w-full items-center gap-2 rounded-lg border px-3 py-2.5 text-left text-sm font-medium transition disabled:cursor-default"
            :class="form.edit_mode === m.value ? 'border-primary bg-primary/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
            @click="form.edit_mode = m.value"
          >
            <UIcon :name="form.edit_mode === m.value ? 'i-lucide-circle-dot' : 'i-lucide-circle'" class="size-4" :class="form.edit_mode === m.value ? 'text-primary' : 'text-(--ui-text-dimmed)'" />
            <UIcon :name="m.icon" class="size-4" />{{ m.label }}
          </button>
        </UTooltip>
      </div>
    </UCard>

    <UCard :ui="{ body: 'space-y-3 sm:p-4' }">
      <div class="flex items-center gap-2">
        <p class="flex items-center gap-1.5 text-sm font-medium">
          {{ t('policy.commandsTitle') }}
          <UTooltip :text="t('policy.commandsHelp', { star: '*' })"><UIcon name="i-lucide-info" class="size-3.5 text-(--ui-text-dimmed)" /></UTooltip>
        </p>
        <UBadge color="neutral" variant="subtle" size="sm" :label="t('policy.commandsCount', { n: form.commands.length })" />
        <UButton v-if="isAdmin" size="xs" color="neutral" variant="ghost" icon="i-lucide-plus" :label="t('policy.addPack')" class="ms-auto" @click="addPack" />
      </div>
      <div class="grid gap-2 sm:grid-cols-2">
        <details v-for="p in packs" :key="p.id" class="group rounded-lg border border-(--ui-border)" :open="p.custom || enabledIn(p) > 0">
          <summary class="flex cursor-pointer list-none items-center gap-2 px-3 py-2">
            <UCheckbox :model-value="packState(p)" :disabled="!isAdmin || !p.commands.length" @click.stop @update:model-value="togglePack(p)" />
            <UIcon :name="p.icon || 'i-lucide-package'" class="size-4 text-primary" />
            <UInput v-if="p.custom && isAdmin" v-model="form.packs.find(x => x.id === p.id)!.label" size="xs" variant="none" class="min-w-0 flex-1 font-medium" @click.stop />
            <span v-else class="min-w-0 flex-1 truncate text-sm font-medium">{{ p.label }}</span>
            <span class="text-xs tabular-nums text-(--ui-text-muted)">{{ enabledIn(p) }}/{{ p.commands.length }}</span>
            <UIcon name="i-lucide-chevron-down" class="size-4 text-(--ui-text-dimmed) transition group-open:rotate-180" />
          </summary>
          <div class="space-y-0.5 border-t border-(--ui-border) px-3 py-2">
            <div v-for="c in p.commands" :key="c" class="group/cmd flex items-center gap-2 py-0.5 text-xs">
              <UCheckbox :model-value="form.commands.includes(c)" :disabled="!isAdmin" @update:model-value="(v: boolean | 'indeterminate') => toggleCmd(c, v === true)" />
              <code class="min-w-0 flex-1 truncate">{{ c }}</code>
              <button v-if="p.custom && isAdmin" type="button" class="invisible text-(--ui-text-dimmed) group-hover/cmd:visible" :title="t('policy.removeCmd')" @click="removeCmd(p, c)">
                <UIcon name="i-lucide-x" class="size-3.5" />
              </button>
            </div>
            <form v-if="p.custom && isAdmin" class="flex items-center gap-1 pt-1" @submit.prevent="addCmd(p)">
              <UInput v-model="newCmd[p.id]" size="xs" :placeholder="t('policy.newCmdPlaceholder')" class="flex-1 font-mono" />
              <UButton type="submit" size="xs" color="neutral" variant="outline" icon="i-lucide-plus" />
              <UButton size="xs" color="error" variant="ghost" icon="i-lucide-trash-2" :title="t('policy.removePack')" @click="removePack(p)" />
            </form>
          </div>
        </details>
      </div>
    </UCard>

    <UCard :ui="{ body: 'space-y-3 sm:p-4' }">
      <p class="flex items-center gap-1.5 text-sm font-medium">
        {{ t('policy.opsTitle') }}
        <UTooltip :text="t('policy.opsHint')"><UIcon name="i-lucide-info" class="size-3.5 text-(--ui-text-dimmed)" /></UTooltip>
      </p>
      <p v-if="!procs.length && !services.length" class="text-sm text-(--ui-text-muted)">{{ t('policy.processesEmpty') }}</p>
      <div class="flex flex-wrap gap-x-4 gap-y-2">
        <UTooltip v-for="p in procs" :key="p.id" :text="`${p.command} · ${t('policy.needs', { cap: needs(p) })}`">
          <label class="flex cursor-pointer items-center gap-1.5 text-sm">
            <UCheckbox :model-value="form.allowed_commands.includes(p.id)" :disabled="!isAdmin" @update:model-value="toggle(form.allowed_commands, p.id)" />
            <UIcon :name="p.kind === 'job' ? 'i-lucide-flask-conical' : 'i-lucide-play'" class="size-3.5 text-(--ui-text-muted)" />{{ p.name }}
          </label>
        </UTooltip>
        <label v-for="sv in services" :key="sv.name" class="flex cursor-pointer items-center gap-1.5 text-sm">
          <UCheckbox :model-value="form.allowed_containers.includes(sv.name)" :disabled="!isAdmin" @update:model-value="toggle(form.allowed_containers, sv.name)" />
          <UIcon name="i-lucide-container" class="size-3.5 text-(--ui-text-muted)" />{{ sv.name }}
        </label>
      </div>
    </UCard>

    <UCard :ui="{ body: 'space-y-3 sm:p-4' }">
      <p class="flex items-center gap-1.5 text-sm font-medium">
        {{ t('policy.denyTitle') }}
        <UTooltip :text="t('policy.denySupports', { star: '*', starstar: '**/', slash: '/' })"><UIcon name="i-lucide-info" class="size-3.5 text-(--ui-text-dimmed)" /></UTooltip>
      </p>
      <div class="flex flex-wrap items-center gap-1.5">
        <UBadge v-for="d in form.deny" :key="d" color="neutral" variant="subtle" class="font-mono">
          {{ d }}
          <button v-if="isAdmin" type="button" class="ms-0.5 text-(--ui-text-dimmed) hover:text-(--ui-text)" :aria-label="t('policy.removeCmd')" @click="form.deny = form.deny.filter(x => x !== d)">
            <UIcon name="i-lucide-x" class="size-3" />
          </button>
        </UBadge>
        <form v-if="isAdmin" class="flex items-center gap-1" @submit.prevent="addDeny">
          <UInput v-model="newDeny" size="xs" placeholder="migrations/" class="w-36 font-mono" />
          <UButton type="submit" size="xs" color="neutral" variant="ghost" icon="i-lucide-plus" />
        </form>
      </div>
    </UCard>

    <div>
      <button type="button" class="flex items-center gap-1 text-sm text-(--ui-text-muted) hover:text-(--ui-text)" @click="advanced = !advanced">
        <UIcon :name="advanced ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-4" />{{ t('policy.advanced') }}
      </button>
      <UCard v-if="advanced" class="mt-2" :ui="{ body: 'space-y-4 sm:p-4' }">
        <UFormField v-if="form.edit_mode === 'worktree'" :label="t('policy.worktreeLinks')" :hint="t('policy.worktreeLinksDesc')">
          <UTextarea v-model="form.links" :rows="2" :disabled="!isAdmin" class="w-full font-mono" placeholder="apps/web/.cache" />
        </UFormField>
        <UTooltip :text="t('policy.isolateClaudeDesc')">
          <USwitch v-model="form.isolate_claude" :disabled="!isAdmin" :label="t('policy.isolateClaude')" />
        </UTooltip>
      </UCard>
    </div>

    <UButton v-if="isAdmin" icon="i-lucide-save" :label="t('policy.save')" :loading="saving" @click="save" />
  </div>
</template>
