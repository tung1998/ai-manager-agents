<script setup lang="ts">
// Other offices (ADR-167): pair with a one-time code, pick which projects of
// this office the other may reach; each project then links to theirs in
// Permissions & commands → Linked projects.
interface RemoteProject { id: string, name: string, description?: string }
interface Peer { id: string, name: string, url: string, self_url: string, shared: string[], remote: RemoteProject[], as: string, created_at: string, last_used_at?: string }

definePageMeta({ admin: true })
const toast = useToast()
const { t, when } = useLang()

const { data, refresh } = await useFetch<{ peers: Peer[], self_name: string }>('/api/peers')
const peers = computed(() => data.value?.peers ?? [])
const { data: projData } = useLiveFetch<{ projects: Project[] }>('/api/projects', { lazy: true })
const projectItems = computed(() => (projData.value?.projects ?? []).map(p => ({ label: p.name, value: p.id })))

const origin = import.meta.client ? window.location.origin : ''

// ---- invite: this office makes a code the other pastes ----
const inviteOpen = ref(false)
const invite = reactive({ name: '', url: '', shared: [] as string[] })
const code = ref('')
const inviting = ref(false)
function openInvite() {
  Object.assign(invite, { name: data.value?.self_name ?? '', url: origin, shared: [] })
  code.value = ''
  inviteOpen.value = true
}
async function makeInvite() {
  inviting.value = true
  try {
    code.value = (await $fetch<{ code: string }>('/api/peers/invite', { method: 'POST', body: invite })).code
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    inviting.value = false
  }
}
async function copyCode() {
  toast.add({ title: (await copyText(code.value)) ? t('peers.copied') : t('peers.copyFailed'), color: 'neutral' })
}

// ---- join: paste the other's code ----
const joinOpen = ref(false)
const join = reactive({ code: '', name: '', url: '' })
const joining = ref(false)
function openJoin() {
  Object.assign(join, { code: '', name: data.value?.self_name ?? '', url: origin })
  joinOpen.value = true
}
async function doJoin() {
  joining.value = true
  try {
    const p = await $fetch<Peer>('/api/peers/join', { method: 'POST', body: join })
    toast.add({ title: t('peers.joined', { name: p.name }), color: 'success' })
    joinOpen.value = false
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    joining.value = false
  }
}

// ---- each paired office ----
const shared = reactive<Record<string, string[]>>({})
watch(peers, list => list.forEach((p) => { shared[p.id] = [...p.shared] }), { immediate: true })
const busy = ref('')
async function act(p: Peer, fn: () => Promise<unknown>, done?: string) {
  busy.value = p.id
  try {
    await fn()
    if (done) toast.add({ title: done, color: 'success' })
    await refresh()
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = ''
  }
}
const saveShared = (p: Peer) => act(p, () => $fetch(`/api/peers/${p.id}/shared`, { method: 'PUT', body: { shared: shared[p.id] } }), t('common.saved'))
const refreshPeer = (p: Peer) => act(p, () => $fetch(`/api/peers/${p.id}/refresh`, { method: 'POST' }))
function removePeer(p: Peer) {
  if (confirm(t('peers.removeConfirm', { name: p.name }))) act(p, () => $fetch(`/api/peers/${p.id}`, { method: 'DELETE' }))
}
const changed = (p: Peer) => JSON.stringify(shared[p.id] ?? []) !== JSON.stringify(p.shared)
</script>

<template>
  <PageShell :title="t('peers.title')">
    <template #actions>
      <InfoTip :text="t('peers.help')" icon-class="size-4 text-(--ui-text-dimmed)" />
      <UButton color="neutral" variant="outline" icon="i-lucide-log-in" :label="t('peers.join')" @click="openJoin" />
      <UButton icon="i-lucide-ticket" :label="t('peers.invite')" @click="openInvite" />
    </template>

    <div class="max-w-4xl space-y-3">
      <p v-if="!peers.length" class="text-sm text-(--ui-text-muted)">
        {{ t('peers.empty') }}
      </p>
      <UCard v-for="p in peers" :key="p.id" :ui="{ body: 'space-y-3 sm:p-4' }">
        <div class="flex flex-wrap items-center gap-2">
          <UIcon name="i-lucide-building-2" class="size-4 text-primary" />
          <span class="font-medium">{{ p.name }}</span>
          <a :href="p.url" target="_blank" rel="noopener" class="truncate text-xs text-(--ui-text-muted) hover:underline">{{ p.url }}</a>
          <span class="ms-auto text-xs text-(--ui-text-dimmed)">{{ p.last_used_at ? t('peers.lastUsed', { when: when(p.last_used_at) }) : t('peers.neverUsed') }}</span>
          <UButton size="xs" color="error" variant="ghost" icon="i-lucide-unlink" :title="t('peers.remove')" :loading="busy === p.id" @click="removePeer(p)" />
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="flex items-center gap-1.5 text-sm">{{ t('peers.shared') }}
            <InfoTip :text="t('peers.sharedHelp', { as: p.as })" icon-class="size-3.5 text-(--ui-text-dimmed)" />
          </span>
          <USelectMenu v-model="shared[p.id]" :items="projectItems" value-key="value" multiple size="sm" :placeholder="t('peers.none')" class="w-full sm:w-72" />
          <UButton v-if="changed(p)" size="xs" icon="i-lucide-save" :label="t('common.save')" :loading="busy === p.id" @click="saveShared(p)" />
        </div>
        <div class="flex flex-wrap items-center gap-1.5">
          <span class="flex items-center gap-1.5 text-sm">{{ t('peers.remote') }}
            <InfoTip :text="t('peers.remoteHelp')" icon-class="size-3.5 text-(--ui-text-dimmed)" />
          </span>
          <UBadge v-for="r in p.remote" :key="r.id" color="neutral" variant="subtle" :title="r.description">
            {{ r.name }}
          </UBadge>
          <span v-if="!p.remote.length" class="text-xs text-(--ui-text-dimmed)">{{ t('peers.none') }}</span>
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-refresh-cw" :title="t('common.refresh')" :loading="busy === p.id" @click="refreshPeer(p)" />
        </div>
      </UCard>
    </div>

    <UModal v-model:open="inviteOpen" :title="t('peers.invite')">
      <template #body>
        <form v-if="!code" id="peer-invite" class="space-y-4" @submit.prevent="makeInvite">
          <UFormField :label="t('peers.selfName')" required>
            <UInput v-model="invite.name" class="w-full" />
          </UFormField>
          <UFormField required>
            <template #label>
              <span class="flex items-center gap-1.5">{{ t('peers.selfUrl') }}<InfoTip :text="t('peers.selfUrlHelp')" icon-class="size-3.5 text-(--ui-text-muted)" /></span>
            </template>
            <UInput v-model="invite.url" class="w-full font-mono" placeholder="https://office.example.com" />
          </UFormField>
          <UFormField :label="t('peers.shared')">
            <USelectMenu v-model="invite.shared" :items="projectItems" value-key="value" multiple :placeholder="t('peers.none')" class="w-full" />
          </UFormField>
        </form>
        <div v-else class="space-y-2">
          <p class="text-sm text-(--ui-text-muted)">{{ t('peers.codeHelp') }}</p>
          <div class="flex items-center gap-2">
            <code class="min-w-0 flex-1 truncate rounded bg-(--ui-bg-elevated) px-2 py-1 text-xs">{{ code }}</code>
            <UButton size="sm" icon="i-lucide-copy" :label="t('common.copy')" @click="copyCode" />
          </div>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" :label="t(code ? 'peers.close' : 'common.cancel')" @click="inviteOpen = false; refresh()" />
          <UButton v-if="!code" type="submit" form="peer-invite" :loading="inviting" :label="t('peers.makeCode')" />
        </div>
      </template>
    </UModal>

    <UModal v-model:open="joinOpen" :title="t('peers.join')">
      <template #body>
        <form id="peer-join" class="space-y-4" @submit.prevent="doJoin">
          <UFormField :label="t('peers.code')" required>
            <UInput v-model="join.code" class="w-full font-mono" placeholder="ofi_…" />
          </UFormField>
          <UFormField :label="t('peers.selfName')" required>
            <UInput v-model="join.name" class="w-full" />
          </UFormField>
          <UFormField required>
            <template #label>
              <span class="flex items-center gap-1.5">{{ t('peers.selfUrl') }}<InfoTip :text="t('peers.selfUrlHelp')" icon-class="size-3.5 text-(--ui-text-muted)" /></span>
            </template>
            <UInput v-model="join.url" class="w-full font-mono" />
          </UFormField>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" :label="t('common.cancel')" @click="joinOpen = false" />
          <UButton type="submit" form="peer-join" :loading="joining" :label="t('peers.pair')" />
        </div>
      </template>
    </UModal>
  </PageShell>
</template>
