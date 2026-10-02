<script setup lang="ts">
// The MCP servers office manages (ADR-091): agent runs reach them through
// the office gateway /mcp/s/<name>, with office's own token.
interface GwTool { name: string, description?: string, read_only?: boolean }
interface GwServer {
  id: string, name: string, kind: string, url: string, headers: Record<string, string>, scope: string, origin: string,
  enabled: boolean, path: string, last_check_at: string | null, last_check_status: '' | 'ok' | 'error', last_check_error: string, tools: GwTool[]
}

const { t, dateLocale } = useLang()
const toast = useToast()
const { data, refresh } = await useLiveFetch<{ servers: GwServer[] }>('/api/mcp/servers')
const list = ref<GwServer[]>([])
watch(data, (d) => { list.value = d?.servers ?? [] }, { immediate: true })
const put = (s: GwServer) => { list.value = list.value.map(x => (x.id === s.id ? s : x)) }
onLiveEvent<GwServer>('mcp.gateway.status', put)

const badge = computed(() => ({
  ok: { color: 'success' as const, icon: 'i-lucide-circle-check', label: t('tools.gwOk') },
  error: { color: 'error' as const, icon: 'i-lucide-circle-x', label: t('tools.gwError') },
  '': { color: 'neutral' as const, icon: 'i-lucide-circle-help', label: t('tools.gwNever') }
}))
const tip = (s: GwServer) => s.last_check_status === 'error'
  ? s.last_check_error
  : s.last_check_at ? t('tools.mcpCheckedAt', { time: new Date(s.last_check_at).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' }) }) : ''

const checking = ref<Record<string, boolean>>({})
const open = ref<Record<string, boolean>>({})
async function check(s: GwServer) {
  checking.value = { ...checking.value, [s.id]: true }
  try {
    const r = await $fetch<{ server: GwServer }>(`/api/mcp/servers/${s.id}/check`, { method: 'POST' })
    put(r.server)
    if (r.server.last_check_status === 'ok') open.value = { ...open.value, [s.id]: true }
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    checking.value = { ...checking.value, [s.id]: false }
  }
}
async function toggle(s: GwServer, enabled: boolean) {
  try {
    put((await $fetch<{ server: GwServer }>(`/api/mcp/servers/${s.id}`, { method: 'PATCH', body: { enabled } })).server)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
async function remove(s: GwServer) {
  if (!confirm(t('tools.gwConfirmDelete', { name: s.name }))) return
  try {
    await $fetch(`/api/mcp/servers/${s.id}`, { method: 'DELETE' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

// ---- form ----
interface Row { key: string, value: string, saved?: string }
const formOpen = ref(false)
const editing = ref<GwServer | null>(null)
const form = ref({ name: '', url: '', rows: [] as Row[] })
const formError = ref('')
const saving = ref(false)
function openForm(s?: GwServer) {
  editing.value = s ?? null
  formError.value = ''
  form.value = s
    ? { name: s.name, url: s.url, rows: Object.entries(s.headers).map(([key, saved]) => ({ key, value: '', saved })) }
    : { name: '', url: '', rows: [{ key: 'Authorization', value: '' }] }
  formOpen.value = true
}
async function save() {
  formError.value = ''
  saving.value = true
  const headers: Record<string, string> = {}
  for (const r of form.value.rows) if (r.key.trim() && (r.value.trim() || r.saved)) headers[r.key.trim()] = r.value.trim()
  try {
    if (editing.value) {
      await $fetch(`/api/mcp/servers/${editing.value.id}`, { method: 'PATCH', body: { url: form.value.url, headers } })
    } else {
      await $fetch('/api/mcp/servers', { method: 'POST', body: { name: form.value.name, url: form.value.url, headers } })
    }
    formOpen.value = false
    toast.add({ title: t('tools.saved', { name: form.value.name }), description: t('tools.gwSavedDesc'), color: 'success' })
    await refresh()
  } catch (e) {
    formError.value = apiError(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="space-y-2">
    <h3 class="flex items-center gap-2 text-sm font-semibold">
      <UIcon name="i-lucide-network" class="text-(--ui-text-muted)" />
      {{ t('tools.gwTitle') }}
      <span class="font-normal text-(--ui-text-muted)">· {{ list.length }}</span>
      <UTooltip :text="t('tools.gwInfo')">
        <UIcon name="i-lucide-info" class="text-(--ui-text-dimmed)" />
      </UTooltip>
      <UButton class="ms-auto" size="xs" icon="i-lucide-plus" :label="t('tools.gwAdd')" @click="openForm()" />
    </h3>
    <p v-if="!list.length" class="rounded-lg border border-dashed border-(--ui-border) px-4 py-3 text-sm text-(--ui-text-muted)">{{ t('tools.gwEmpty') }}</p>
    <div v-else class="divide-y divide-(--ui-border) rounded-lg border border-(--ui-border)">
      <div v-for="s in list" :key="s.id" class="px-4 py-2.5">
        <div class="flex items-center gap-3">
          <UIcon name="i-lucide-plug" class="size-4 shrink-0 text-primary" />
          <div class="min-w-0 flex-1">
            <p class="truncate font-mono text-sm font-medium">{{ s.name }}</p>
            <p class="truncate font-mono text-xs text-(--ui-text-muted)">{{ s.url }}</p>
          </div>
          <UButton v-if="s.tools.length" size="xs" color="neutral" variant="subtle" :label="t('tools.gwTools', { n: s.tools.length })"
                   :trailing-icon="open[s.id] ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'" @click="open = { ...open, [s.id]: !open[s.id] }" />
          <UTooltip :text="tip(s)" :disabled="!tip(s)">
            <UBadge :color="badge[s.last_check_status].color" variant="subtle" size="sm" :icon="badge[s.last_check_status].icon" :label="badge[s.last_check_status].label" />
          </UTooltip>
          <USwitch :model-value="s.enabled" size="sm" :aria-label="t('tools.gwEnabled')" @update:model-value="v => toggle(s, v)" />
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-refresh-cw" :aria-label="t('tools.mcpCheck')" :loading="checking[s.id]" @click="check(s)" />
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-pencil" :aria-label="t('common.edit')" @click="openForm(s)" />
          <UButton size="xs" color="error" variant="ghost" icon="i-lucide-trash-2" :aria-label="t('common.delete')" @click="remove(s)" />
        </div>
        <p v-if="s.last_check_status === 'error'" class="mt-1 text-xs text-(--ui-error)">{{ s.last_check_error }}</p>
        <ul v-if="open[s.id] && s.tools.length" class="mt-2 space-y-1 ps-7">
          <li v-for="tl in s.tools" :key="tl.name" class="flex items-baseline gap-2 text-xs">
            <code class="shrink-0">mcp__{{ s.name }}__{{ tl.name }}</code>
            <UBadge v-if="tl.read_only" color="neutral" variant="subtle" size="sm" :label="t('tools.gwReadOnly')" />
            <span class="truncate text-(--ui-text-muted)">{{ tl.description }}</span>
          </li>
        </ul>
      </div>
    </div>

    <UModal v-model:open="formOpen" :title="editing ? t('tools.editTitle', { name: editing.name }) : t('tools.gwAddTitle')">
      <template #body>
        <form id="gw-form" class="space-y-4" @submit.prevent="save">
          <UFormField :label="t('common.name')">
            <template #hint>
              <UTooltip :text="t('tools.gwNameInfo')"><UIcon name="i-lucide-info" class="text-(--ui-text-dimmed)" /></UTooltip>
            </template>
            <UInput v-model="form.name" :disabled="!!editing" placeholder="context7" class="w-full font-mono" required />
          </UFormField>
          <UFormField label="URL">
            <UInput v-model="form.url" placeholder="https://mcp.context7.com/mcp" class="w-full font-mono" required />
          </UFormField>
          <UFormField :label="t('tools.gwHeaders')">
            <template #hint>
              <UTooltip :text="t('tools.gwHeadersInfo')"><UIcon name="i-lucide-info" class="text-(--ui-text-dimmed)" /></UTooltip>
            </template>
            <div class="space-y-2">
              <div v-for="(r, i) in form.rows" :key="i" class="flex gap-2">
                <UInput v-model="r.key" placeholder="Authorization" class="w-40 font-mono" />
                <UInput v-model="r.value" type="password" autocomplete="off" :placeholder="r.saved ? t('tools.gwKeepSaved', { hint: r.saved }) : 'Bearer …'" class="flex-1 font-mono" />
                <UButton color="neutral" variant="ghost" icon="i-lucide-x" :aria-label="t('common.delete')" @click="form.rows.splice(i, 1)" />
              </div>
              <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-plus" :label="t('tools.gwAddHeader')" @click="form.rows.push({ key: '', value: '' })" />
            </div>
          </UFormField>
          <UAlert v-if="formError" color="error" variant="subtle" icon="i-lucide-circle-alert" :title="formError" />
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" :label="t('common.cancel')" @click="formOpen = false" />
          <UButton type="submit" form="gw-form" :label="t('common.save')" :loading="saving" />
        </div>
      </template>
    </UModal>
  </section>
</template>
