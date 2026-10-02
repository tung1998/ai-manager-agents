<script setup lang="ts">
// The MCP servers office manages (ADR-091, ADR-092): agent runs reach them
// through the office gateway /mcp/s/<name>, with office's own token. HTTP
// servers may log in with OAuth (office keeps and refreshes the token);
// stdio servers are processes office runs itself.
interface GwTool { name: string, description?: string, read_only?: boolean }
interface GwOAuth { required: boolean, logged_in: boolean, expired: boolean, expires_at: string | null, client_manual: boolean, client_id: string, has_secret: boolean, dynamic: boolean }
type GwStatus = '' | 'ok' | 'error' | 'needs_login'
interface GwServer {
  id: string, name: string, kind: 'http' | 'stdio', url: string, headers: Record<string, string>,
  command: string, args: string[], env: Record<string, string>, oauth: GwOAuth | null, scope: string, origin: string,
  enabled: boolean, path: string, last_check_at: string | null, last_check_status: GwStatus, last_check_error: string, tools: GwTool[]
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
  needs_login: { color: 'warning' as const, icon: 'i-lucide-key-round', label: t('tools.gwNeedsLogin') },
  '': { color: 'neutral' as const, icon: 'i-lucide-circle-help', label: t('tools.gwNever') }
}))
const tip = (s: GwServer) => s.last_check_status === 'error' || s.last_check_status === 'needs_login'
  ? s.last_check_error
  : s.last_check_at ? t('tools.mcpCheckedAt', { time: new Date(s.last_check_at).toLocaleString(dateLocale.value, { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' }) }) : ''
const target = (s: GwServer) => s.kind === 'stdio' ? [s.command, ...s.args].join(' ') : s.url
// a login is offered once office found the server's authorization server
const canLogin = (s: GwServer) => s.kind === 'http' && (s.last_check_status === 'needs_login' || !!s.oauth?.required)
const loggedIn = (s: GwServer) => !!s.oauth?.logged_in && !s.oauth.expired

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

// ---- OAuth login ----
// some authorization servers only accept an https or localhost callback
const plainRemote = computed(() => import.meta.client && location.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(location.hostname))
const connecting = ref<Record<string, boolean>>({})
async function connect(s: GwServer) {
  // open the tab now: a tab opened after an await is blocked as a popup
  const tab = window.open('', '_blank')
  connecting.value = { ...connecting.value, [s.id]: true }
  try {
    const r = await $fetch<{ url: string }>(`/api/mcp/servers/${s.id}/oauth/start`, { method: 'POST', body: { origin: location.origin } })
    if (tab) tab.location.href = r.url
    else location.href = r.url
  } catch (e) {
    tab?.close()
    toast.add({ title: apiError(e), description: plainRemote.value ? t('tools.gwLocalhostHint') : undefined, color: 'error' })
  } finally {
    connecting.value = { ...connecting.value, [s.id]: false }
  }
}
async function logout(s: GwServer) {
  if (!confirm(t('tools.gwConfirmLogout', { name: s.name }))) return
  try {
    put((await $fetch<{ server: GwServer }>(`/api/mcp/servers/${s.id}/oauth/logout`, { method: 'POST' })).server)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

// ---- form ----
interface Row { key: string, value: string, saved?: string }
const formOpen = ref(false)
const editing = ref<GwServer | null>(null)
const blank = () => ({ name: '', kind: 'http' as string, url: '', rows: [] as Row[], command: '', args: '', env: [] as Row[], clientOpen: false, clientId: '', clientSecret: '', hasSecret: false })
const form = ref(blank())
const formError = ref('')
const saving = ref(false)
const kinds = computed(() => [{ label: 'HTTP', value: 'http' }, { label: t('tools.gwStdio'), value: 'stdio' }])
const rowsOf = (m: Record<string, string>) => Object.entries(m).map(([key, saved]) => ({ key, value: '', saved }))
function openForm(s?: GwServer) {
  editing.value = s ?? null
  formError.value = ''
  form.value = s
    ? { ...blank(), name: s.name, kind: s.kind, url: s.url, rows: rowsOf(s.headers), command: s.command, args: s.args.join('\n'), env: rowsOf(s.env),
        clientOpen: !!s.oauth?.client_manual, clientId: s.oauth?.client_id ?? '', hasSecret: !!s.oauth?.has_secret }
    : { ...blank(), rows: [{ key: 'Authorization', value: '' }] }
  formOpen.value = true
}
const sealed = (rows: Row[]) => {
  const out: Record<string, string> = {}
  for (const r of rows) if (r.key.trim() && (r.value.trim() || r.saved)) out[r.key.trim()] = r.value.trim()
  return out
}
async function save() {
  formError.value = ''
  saving.value = true
  const f = form.value
  const body: Record<string, unknown> = f.kind === 'stdio'
    ? { command: f.command, args: f.args.split('\n').map(a => a.trim()).filter(Boolean), env: sealed(f.env) }
    : { url: f.url, headers: sealed(f.rows), oauth_client_id: f.clientOpen ? f.clientId : '', oauth_client_secret: f.clientOpen ? f.clientSecret : '' }
  try {
    if (editing.value) {
      await $fetch(`/api/mcp/servers/${editing.value.id}`, { method: 'PATCH', body })
    } else {
      await $fetch('/api/mcp/servers', { method: 'POST', body: { ...body, name: f.name, kind: f.kind } })
    }
    formOpen.value = false
    toast.add({ title: t('tools.saved', { name: f.name }), description: t('tools.gwSavedDesc'), color: 'success' })
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
        <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
          <UIcon :name="s.kind === 'stdio' ? 'i-lucide-terminal' : 'i-lucide-plug'" class="size-4 shrink-0 text-primary" />
          <div class="min-w-0 flex-1">
            <p class="truncate font-mono text-sm font-medium">{{ s.name }}</p>
            <p class="truncate font-mono text-xs text-(--ui-text-muted)">{{ target(s) }}</p>
          </div>
          <div class="flex items-center gap-2">
            <UButton v-if="s.tools.length" size="xs" color="neutral" variant="subtle" :label="t('tools.gwTools', { n: s.tools.length })"
                     :trailing-icon="open[s.id] ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'" @click="open = { ...open, [s.id]: !open[s.id] }" />
            <UButton v-if="canLogin(s) && !loggedIn(s)" size="xs" icon="i-lucide-log-in" :loading="connecting[s.id]"
                     :label="s.oauth?.logged_in ? t('tools.gwReconnect') : t('tools.gwConnect')" @click="connect(s)" />
            <UTooltip :text="tip(s)" :disabled="!tip(s)">
              <UBadge :color="badge[s.last_check_status].color" variant="subtle" size="sm" :icon="badge[s.last_check_status].icon" :label="badge[s.last_check_status].label" />
            </UTooltip>
            <USwitch :model-value="s.enabled" size="sm" :aria-label="t('tools.gwEnabled')" @update:model-value="v => toggle(s, v)" />
            <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-refresh-cw" :aria-label="t('tools.mcpCheck')" :loading="checking[s.id]" @click="check(s)" />
            <UButton v-if="loggedIn(s)" size="xs" color="neutral" variant="ghost" icon="i-lucide-log-out" :aria-label="t('tools.gwLogout')" @click="logout(s)" />
            <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-pencil" :aria-label="t('common.edit')" @click="openForm(s)" />
            <UButton size="xs" color="error" variant="ghost" icon="i-lucide-trash-2" :aria-label="t('common.delete')" @click="remove(s)" />
          </div>
        </div>
        <p v-if="s.last_check_status === 'error' || s.last_check_status === 'needs_login'" class="mt-1 text-xs"
           :class="s.last_check_status === 'error' ? 'text-(--ui-error)' : 'text-(--ui-warning)'">
          {{ s.last_check_error }}
          <span v-if="canLogin(s) && plainRemote" class="text-(--ui-text-muted)"> · {{ t('tools.gwLocalhostHint') }}</span>
        </p>
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
          <UFormField v-if="!editing" :label="t('tools.gwKind')">
            <URadioGroup v-model="form.kind" :items="kinds" orientation="horizontal" />
          </UFormField>
          <UFormField :label="t('common.name')">
            <template #hint>
              <UTooltip :text="t('tools.gwNameInfo')"><UIcon name="i-lucide-info" class="text-(--ui-text-dimmed)" /></UTooltip>
            </template>
            <UInput v-model="form.name" :disabled="!!editing" :placeholder="form.kind === 'stdio' ? 'filesystem' : 'context7'" class="w-full font-mono" required />
          </UFormField>

          <template v-if="form.kind === 'stdio'">
            <UFormField :label="t('tools.gwCommand')">
              <template #hint>
                <UTooltip :text="t('tools.gwCommandInfo')"><UIcon name="i-lucide-info" class="text-(--ui-text-dimmed)" /></UTooltip>
              </template>
              <UInput v-model="form.command" placeholder="npx" class="w-full font-mono" required />
            </UFormField>
            <UFormField :label="t('tools.gwArgs')" :hint="t('tools.gwArgsHint')">
              <UTextarea v-model="form.args" :rows="3" autoresize placeholder="-y&#10;@modelcontextprotocol/server-filesystem&#10;/Users/me/docs" class="w-full font-mono" />
            </UFormField>
            <UFormField :label="t('tools.gwEnv')">
              <template #hint>
                <UTooltip :text="t('tools.gwEnvInfo')"><UIcon name="i-lucide-info" class="text-(--ui-text-dimmed)" /></UTooltip>
              </template>
              <div class="space-y-2">
                <div v-for="(r, i) in form.env" :key="i" class="flex gap-2">
                  <UInput v-model="r.key" placeholder="API_KEY" class="w-40 font-mono" />
                  <UInput v-model="r.value" type="password" autocomplete="off" :placeholder="r.saved ? t('tools.gwKeepSaved', { hint: r.saved }) : '…'" class="flex-1 font-mono" />
                  <UButton color="neutral" variant="ghost" icon="i-lucide-x" :aria-label="t('common.delete')" @click="form.env.splice(i, 1)" />
                </div>
                <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-plus" :label="t('tools.gwAddEnv')" @click="form.env.push({ key: '', value: '' })" />
              </div>
            </UFormField>
          </template>

          <template v-else>
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
            <div class="space-y-2">
              <UCheckbox v-model="form.clientOpen" :label="t('tools.gwOwnClient')">
                <template #description>
                  <span class="text-xs text-(--ui-text-muted)">{{ t('tools.gwOwnClientInfo') }}</span>
                </template>
              </UCheckbox>
              <div v-if="form.clientOpen" class="flex gap-2">
                <UInput v-model="form.clientId" placeholder="client_id" class="flex-1 font-mono" />
                <UInput v-model="form.clientSecret" type="password" autocomplete="off" :placeholder="form.hasSecret ? t('tools.gwKeepSecret') : 'client_secret'" class="flex-1 font-mono" />
              </div>
            </div>
          </template>
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
