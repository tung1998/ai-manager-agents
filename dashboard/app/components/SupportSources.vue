<script setup lang="ts">
// Where customers' messages come from (ADR-173): an API or a form, each
// answered by an automation (an agent or a workflow), sent at once (auto)
// or after a person approves (approve). Its token shows once.
const props = defineProps<{ projectId: string }>()
const { t } = useLang()
const toast = useToast()
const { isAdmin } = useAuth()
const base = computed(() => `/api/projects/${props.projectId}/support/sources`)
const { data, error } = useLiveFetch<{ sources: SupportSource[] }>(() => base.value, { lazy: true })
const { data: autos } = useLiveFetch<{ automations: Automation[] }>(() => `/api/projects/${props.projectId}/automations`, { lazy: true })
const answerers = computed(() => (autos.value?.automations ?? []).filter(a => a.action === 'chat' || a.action === 'workflow'))
const autoName = (id: string) => answerers.value.find(a => a.id === id)?.name ?? t('sup.noAutomation')
const modes = computed(() => [{ label: t('sup.mode.approve'), value: 'approve' }, { label: t('sup.mode.auto'), value: 'auto' }])
const hookURL = (id: string) => `${window.location.origin}/hooks/support/${id}`

const open = ref(false)
const editing = ref<SupportSource | null>(null)
const form = reactive({ name: '', automation_id: '', mode: 'approve' as SupportSource['mode'], rate_per_min: 30 })
function openForm(s?: SupportSource) {
  editing.value = s ?? null
  Object.assign(form, s
    ? { name: s.name, automation_id: s.automation_id, mode: s.mode, rate_per_min: s.rate_per_min }
    : { name: '', automation_id: answerers.value[0]?.id ?? '', mode: 'approve', rate_per_min: 30 })
  open.value = true
}
const saving = ref(false)
const secret = ref<{ id: string, token: string } | null>(null)
async function save() {
  if (saving.value) return
  saving.value = true
  try {
    const body = { ...form, rate_per_min: Number(form.rate_per_min) }
    if (editing.value) {
      await $fetch(`${base.value}/${editing.value.id}`, { method: 'PATCH', body })
    } else {
      const r = await $fetch<{ source: SupportSource, token: string }>(base.value, { method: 'POST', body: { ...body, kind: 'api' } })
      secret.value = { id: r.source.id, token: r.token }
    }
    open.value = false
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    saving.value = false
  }
}
async function toggle(s: SupportSource, enabled: boolean) {
  try {
    await $fetch(`${base.value}/${s.id}`, { method: 'PATCH', body: { enabled } })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
async function rotate(s: SupportSource) {
  if (!confirm(t('sup.rotateConfirm'))) return
  try {
    const r = await $fetch<{ token: string }>(`${base.value}/${s.id}/token`, { method: 'POST' })
    secret.value = { id: s.id, token: r.token }
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}
const curl = computed(() => secret.value
  ? `curl -X POST ${hookURL(secret.value.id)} -H 'Authorization: Bearer ${secret.value.token}' -H 'Content-Type: application/json' \\\n  -d '{"external_id":"review-1","thread":"order-77","text":"Wrong size delivered","customer":{"name":"An","email":"an@example.com"},"wait":60}'`
  : '')
async function copy(text: string) {
  if (await copyText(text)) toast.add({ title: t('auto.copied'), color: 'success' })
}
</script>

<template>
  <div class="max-w-4xl space-y-3">
    <div class="flex items-center gap-2">
      <p class="flex-1 text-sm text-(--ui-text-muted)">{{ t('sup.sourcesHelp') }}</p>
      <UButton v-if="isAdmin" size="sm" icon="i-lucide-plus" :label="t('sup.addSource')" @click="openForm()" />
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :title="apiError(error)" />
    <div v-else-if="!data" class="rounded-lg border border-(--ui-border)"><LoadingRows :n="2" /></div>
    <p v-else-if="!data.sources.length" class="rounded-lg border border-dashed border-(--ui-border) p-4 text-sm text-(--ui-text-muted)">{{ t('sup.noSources') }}</p>
    <div v-for="s in data?.sources ?? []" :key="s.id" class="space-y-2 rounded-lg border border-(--ui-border) p-3">
      <div class="flex flex-wrap items-center gap-2">
        <UIcon name="i-lucide-webhook" class="size-4 text-primary" />
        <p class="font-medium">{{ s.name }}</p>
        <UBadge size="sm" variant="subtle" color="neutral" :label="t(`sup.mode.${s.mode}` as 'sup.mode.auto')" />
        <span class="text-xs text-(--ui-text-muted)">→ {{ autoName(s.automation_id) }} · {{ t('sup.perMin', { n: s.rate_per_min }) }}</span>
        <div v-if="isAdmin" class="ms-auto flex items-center gap-1">
          <USwitch :model-value="s.enabled" size="sm" :aria-label="t('sup.enabled')" @update:model-value="v => toggle(s, v)" />
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-pencil" :aria-label="t('auto.edit')" @click="openForm(s)" />
          <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-key-round" :aria-label="t('sup.rotate')" @click="rotate(s)" />
        </div>
      </div>
      <div class="flex items-center gap-2">
        <code class="min-w-0 flex-1 truncate rounded bg-(--ui-bg-elevated) px-2 py-1 text-xs">POST {{ hookURL(s.id) }}</code>
        <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-copy" :aria-label="t('auto.copy')" @click="copy(hookURL(s.id))" />
      </div>
    </div>

    <UModal v-model:open="open" :title="editing ? t('sup.editSource') : t('sup.addSource')">
      <template #body>
        <form id="sup-source" class="space-y-3" @submit.prevent="save">
          <UFormField :label="t('sup.name')" required><UInput v-model="form.name" class="w-full" :placeholder="t('sup.namePlaceholder')" /></UFormField>
          <UFormField :label="t('sup.automation')" :hint="t('sup.automationHint')" required>
            <USelect v-model="form.automation_id" :items="answerers.map(a => ({ label: a.name, value: a.id }))" class="w-full" :placeholder="t('sup.noAutomation')" />
          </UFormField>
          <div class="grid gap-3 sm:grid-cols-2">
            <UFormField :label="t('sup.modeLabel')"><USelect v-model="form.mode" :items="modes" class="w-full" /></UFormField>
            <UFormField :label="t('sup.rate')"><UInput v-model="form.rate_per_min" type="number" min="1" max="600" class="w-full" /></UFormField>
          </div>
        </form>
      </template>
      <template #footer>
        <UButton type="submit" form="sup-source" :label="t('common.save')" :loading="saving" :disabled="!form.name || !form.automation_id" />
      </template>
    </UModal>

    <!-- the token, once -->
    <UModal :open="!!secret" :dismissible="false" :close="false" :title="t('sup.tokenTitle')" :description="t('sup.tokenDesc')">
      <template #body>
        <div v-if="secret" class="space-y-3 text-sm">
          <div v-for="row in [{ label: 'URL', text: hookURL(secret.id) }, { label: 'Token', text: secret.token }, { label: 'curl', text: curl }]" :key="row.label" class="space-y-1">
            <p class="text-xs font-medium text-(--ui-text-muted)">{{ row.label }}</p>
            <div class="flex items-start gap-2">
              <code class="min-w-0 flex-1 whitespace-pre-wrap break-all rounded bg-(--ui-bg-elevated) px-2 py-1 text-xs">{{ row.text }}</code>
              <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-copy" :aria-label="t('auto.copy')" @click="copy(row.text)" />
            </div>
          </div>
        </div>
      </template>
      <template #footer>
        <UButton :label="t('auto.savedIt')" @click="secret = null" />
      </template>
    </UModal>
  </div>
</template>
