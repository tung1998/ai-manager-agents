<script setup lang="ts">
// A project's limits on its agents: the highest package any agent may use
// here, what they may run or restart on their own, and files no diff may touch.
interface Policy { max_level: PermLevel, allowed_commands: string[], commands: string[], packs: CommandPack[], allowed_containers: string[], deny_paths: string[] }
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

const form = reactive({ max_level: 'propose' as PermLevel, allowed_commands: [] as string[], commands: [] as string[], packs: [] as CommandPack[], allowed_containers: [] as string[], deny: '' })
watch(data, (d) => {
  if (!d) return
  form.max_level = d.policy.max_level
  form.allowed_commands = [...d.policy.allowed_commands]
  form.commands = [...d.policy.commands]
  form.packs = JSON.parse(JSON.stringify(d.policy.packs))
  form.allowed_containers = [...d.policy.allowed_containers]
  form.deny = d.policy.deny_paths.join('\n')
}, { immediate: true })

function toggle(list: string[], v: string) {
  const i = list.indexOf(v)
  if (i >= 0) list.splice(i, 1)
  else list.push(v)
}
const saving = ref(false)
async function save() {
  saving.value = true
  try {
    await $fetch(`/api/projects/${props.projectId}/policy`, {
      method: 'PUT',
      body: { max_level: form.max_level, allowed_commands: form.allowed_commands, commands: form.commands, packs: form.packs.filter(p => p.label.trim()), allowed_containers: form.allowed_containers, deny_paths: form.deny.split('\n').map(s => s.trim()).filter(Boolean) }
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
  <div class="max-w-4xl space-y-6">
    <p class="text-sm text-(--ui-text-muted)">
      {{ t('policy.intro') }}
    </p>

    <section class="space-y-2">
      <p class="text-sm font-medium">{{ t('policy.maxLevel') }}</p>
      <div class="flex flex-wrap gap-1 rounded-lg bg-(--ui-bg-elevated) p-1">
        <button
          v-for="p in permLevels" :key="p.level" type="button" :disabled="!isAdmin"
          class="flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition disabled:cursor-default"
          :class="form.max_level === p.level ? 'bg-(--ui-bg) text-(--ui-text-highlighted) shadow-sm' : 'text-(--ui-text-muted) hover:text-(--ui-text)'"
          @click="form.max_level = p.level"
        >
          <UIcon :name="p.icon" class="size-3.5" :class="form.max_level === p.level ? 'text-primary' : ''" />{{ p.label }}
        </button>
      </div>
      <p class="text-xs text-(--ui-text-muted)">
        {{ t('policy.maxCan') }}
        <template v-for="(c, i) in permCaps.filter(x => permRank(x.min) <= permRank(form.max_level))" :key="c.id">{{ i ? ', ' : '' }}{{ c.label.toLowerCase() }}</template>
        <template v-if="form.max_level === 'read'">{{ t('policy.maxCanNone') }}</template>.
      </p>
    </section>

    <section class="space-y-2">
      <div class="flex items-center gap-2">
        <p class="text-sm font-medium">{{ t('policy.commandsTitle') }}</p>
        <UBadge color="neutral" variant="subtle" size="sm" :label="t('policy.commandsCount', { n: form.commands.length })" />
        <UButton v-if="isAdmin" size="xs" color="neutral" variant="ghost" icon="i-lucide-plus" :label="t('policy.addPack')" class="ms-auto" @click="addPack" />
      </div>
      <p class="text-xs text-(--ui-text-muted)">
        {{ t('policy.commandsHelp', { star: '*' }) }}
      </p>
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
    </section>

    <section class="space-y-2">
      <p class="text-sm font-medium">{{ t('policy.processesTitle') }} <span class="font-normal text-(--ui-text-muted)">{{ t('policy.processesSource') }}</span></p>
      <p v-if="!procs.length" class="text-sm text-(--ui-text-muted)">{{ t('policy.processesEmpty') }}</p>
      <label v-for="p in procs" :key="p.id" class="flex cursor-pointer items-center gap-2 text-sm">
        <UCheckbox :model-value="form.allowed_commands.includes(p.id)" :disabled="!isAdmin" @update:model-value="toggle(form.allowed_commands, p.id)" />
        <span class="font-medium">{{ p.name }}</span>
        <code class="truncate text-xs text-(--ui-text-muted)">{{ p.command }}</code>
        <UBadge color="neutral" variant="subtle" size="sm" :label="t('policy.needs', { cap: needs(p) })" class="ms-auto shrink-0" />
      </label>
    </section>

    <section v-if="services.length" class="space-y-2">
      <p class="text-sm font-medium">{{ t('policy.containersTitle') }} <span class="font-normal text-(--ui-text-muted)">{{ t('policy.containersHelp') }}</span></p>
      <div class="flex flex-wrap gap-3">
        <label v-for="s in services" :key="s.name" class="flex cursor-pointer items-center gap-2 text-sm">
          <UCheckbox :model-value="form.allowed_containers.includes(s.name)" :disabled="!isAdmin" @update:model-value="toggle(form.allowed_containers, s.name)" />
          {{ s.name }}
        </label>
      </div>
    </section>

    <section class="space-y-2">
      <p class="text-sm font-medium">{{ t('policy.denyTitle') }} <span class="font-normal text-(--ui-text-muted)">{{ t('policy.denyHelp') }}</span></p>
      <UTextarea v-model="form.deny" :rows="5" :disabled="!isAdmin" class="w-full font-mono text-xs" placeholder=".env&#10;**/*.pem&#10;migrations/&#10;nuxt.config.ts" />
      <p class="text-xs text-(--ui-text-muted)">{{ t('policy.denySupports', { star: '*', starstar: '**/', slash: '/' }) }}</p>
    </section>

    <UButton v-if="isAdmin" icon="i-lucide-save" :label="t('policy.save')" :loading="saving" @click="save" />
  </div>
</template>
