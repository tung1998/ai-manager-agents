<script setup lang="ts">
// Docker Compose services of a project: state, ports, CPU/RAM, start/stop, logs.
import type { Attachment } from './PromptInput.vue'

interface Port { url: string, published: number, target: number, protocol: string }
interface Stats { cpu: string, mem: string, mem_perc: string, net: string, block: string, pids: string }
interface Container { id: string, name: string, image: string, state: string, status: string, health?: string, ports: Port[], stats?: Stats }
interface Service { name: string, container?: Container }
interface ActionState { status: string, exit_code?: number, started_at?: string }
interface View { docker: { available: boolean, version?: string, error?: string }, files: string[], file: string, services: Service[], action?: ActionState }

const props = defineProps<{ projectId: string }>()
const emit = defineEmits<{ askAgent: [text: string, files: Attachment[]] }>()
const toast = useToast()
const { isAdmin } = useAuth()

const file = ref('')
const { data, refresh, status } = await useFetch<View>(() => `/api/projects/${props.projectId}/compose`, { query: { file } })
watch(() => data.value?.file, (f) => { if (f && !file.value) file.value = f })
const services = computed(() => data.value?.services ?? [])
const running = computed(() => services.value.filter(s => s.container?.state === 'running').length)
const actionRunning = computed(() => data.value?.action?.status === 'running')

let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => { poll = setInterval(() => { if (status.value !== 'pending') refresh() }, actionRunning.value ? 2000 : 5000) })
onBeforeUnmount(() => clearInterval(poll))

// what the log pane shows: a service's logs, or the last docker action's output
const view = ref<{ kind: 'service', name: string } | { kind: 'action' } | null>(null)
watch(services, (list) => {
  if (!view.value && list[0]) view.value = { kind: 'service', name: list.find(s => s.container?.state === 'running')?.name ?? list[0].name }
}, { immediate: true })
const logUrl = computed(() => {
  const v = view.value
  if (!v) return null
  if (v.kind === 'action') return `/api/projects/${props.projectId}/compose/action/stream`
  return `/api/projects/${props.projectId}/compose/logs?file=${encodeURIComponent(file.value)}&service=${encodeURIComponent(v.name)}`
})

const busy = ref('')
async function act(action: string, service = '') {
  if (action === 'down' && !service && !confirm('Dừng và gỡ toàn bộ container của stack? Volume vẫn được giữ.')) return
  busy.value = action + service
  try {
    await $fetch(`/api/projects/${props.projectId}/compose/action`, { method: 'POST', body: { file: file.value, action, service } })
    view.value = { kind: 'action' }
    setTimeout(refresh, 800)
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    busy.value = ''
  }
}
async function askAgent(s: Service) {
  try {
    const att = await $fetch<Attachment>(`/api/projects/${props.projectId}/compose/log-attachment`, { method: 'POST', body: { file: file.value, service: s.name } })
    const why = s.container ? `đang ở trạng thái "${s.container.status}"` : 'chưa chạy được'
    emit('askAgent', `Container "${s.name}" (docker compose, ${file.value}) ${why}. Xem log đính kèm, tìm nguyên nhân và đề xuất cách sửa.`, [att])
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

function stateOf(s: Service) {
  const c = s.container
  if (!c) return { label: 'Chưa tạo', color: 'neutral' as const, dot: 'bg-(--ui-text-dimmed)' }
  if (c.state === 'running') {
    if (c.health === 'unhealthy') return { label: 'Không khỏe', color: 'error' as const, dot: 'bg-(--ui-error)' }
    if (c.health === 'starting') return { label: 'Đang khởi động', color: 'warning' as const, dot: 'bg-(--ui-warning)' }
    return { label: 'Đang chạy', color: 'success' as const, dot: 'bg-(--ui-success)' }
  }
  if (c.state === 'restarting') return { label: 'Đang chạy lại', color: 'warning' as const, dot: 'bg-(--ui-warning)' }
  if (c.state === 'exited' && !/\(0\)/.test(c.status)) return { label: 'Lỗi', color: 'error' as const, dot: 'bg-(--ui-error)' }
  return { label: 'Đã dừng', color: 'neutral' as const, dot: 'bg-(--ui-text-dimmed)' }
}
const stackMenu = computed(() => [[
  { label: 'Tải image mới (pull)', icon: 'i-lucide-download', onSelect: () => act('pull') },
  { label: 'Build lại image', icon: 'i-lucide-hammer', onSelect: () => act('build') },
  { label: 'Xem output thao tác', icon: 'i-lucide-terminal', onSelect: () => { view.value = { kind: 'action' } } }
], [
  { label: 'Gỡ stack (down)', icon: 'i-lucide-trash-2', color: 'error' as const, onSelect: () => act('down') }
]])
</script>

<template>
  <div class="space-y-4">
    <div v-if="!data?.files.length" class="rounded-lg border border-dashed border-(--ui-border) p-10 text-center">
      <UIcon name="i-lucide-container" class="mx-auto size-8 text-(--ui-text-dimmed)" />
      <p class="mt-2 font-medium">Project không có file docker compose</p>
      <p class="text-sm text-(--ui-text-muted)">Office tìm docker-compose.yml, docker-compose.yaml, compose.yml, compose.yaml ở thư mục gốc.</p>
    </div>

    <template v-else>
      <div class="flex flex-wrap items-center gap-2">
        <USelect v-if="data.files.length > 1" v-model="file" :items="data.files" size="sm" class="w-56" />
        <UBadge v-else color="neutral" variant="subtle" icon="i-lucide-file-code" :label="data.file" />
        <UBadge v-if="data.docker.available" color="success" variant="subtle" icon="i-lucide-container" :label="`Docker ${data.docker.version}`" />
        <span v-if="services.length" class="text-sm text-(--ui-text-muted)">{{ running }}/{{ services.length }} đang chạy</span>
        <UBadge v-if="actionRunning" color="info" variant="subtle" icon="i-lucide-loader-circle" label="Đang chạy thao tác…" :ui="{ leadingIcon: 'animate-spin' }" />
        <div v-if="isAdmin && data.docker.available" class="ms-auto flex gap-2">
          <UButton size="sm" icon="i-lucide-play" label="Bật stack" :loading="busy === 'up'" :disabled="actionRunning" @click="act('up')" />
          <UButton size="sm" color="neutral" variant="outline" icon="i-lucide-square" label="Dừng stack" :loading="busy === 'stop'" :disabled="actionRunning" @click="act('stop')" />
          <UDropdownMenu :items="stackMenu" :content="{ align: 'end' }">
            <UButton size="sm" color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Thao tác khác" :disabled="actionRunning" />
          </UDropdownMenu>
        </div>
      </div>

      <UAlert v-if="data.docker.error" color="warning" variant="subtle" icon="i-lucide-triangle-alert" title="Không đọc được Docker" :description="data.docker.error" />

      <div v-if="services.length" class="grid gap-4 lg:grid-cols-[minmax(0,24rem)_minmax(0,1fr)]">
        <div class="space-y-2">
          <div
            v-for="s in services" :key="s.name" role="button" tabindex="0"
            class="rounded-lg border p-3 transition"
            :class="view?.kind === 'service' && view.name === s.name ? 'border-(--ui-primary) bg-(--ui-primary)/5' : 'border-(--ui-border) hover:border-(--ui-border-accented)'"
            @click="view = { kind: 'service', name: s.name }" @keydown.enter="view = { kind: 'service', name: s.name }"
          >
            <div class="flex items-center gap-2">
              <span class="size-2 shrink-0 rounded-full" :class="[stateOf(s).dot, { 'animate-pulse': s.container?.state === 'running' }]" />
              <p class="min-w-0 flex-1 truncate font-medium">{{ s.name }}</p>
              <UBadge :color="stateOf(s).color" variant="subtle" size="sm" :label="stateOf(s).label" />
            </div>
            <p v-if="s.container" class="mt-1 truncate text-xs text-(--ui-text-muted)">{{ s.container.image }} · {{ s.container.status }}</p>
            <div v-if="s.container?.stats" class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-(--ui-text-muted) tabular-nums">
              <span>CPU {{ s.container.stats.cpu }}</span>
              <span>RAM {{ s.container.stats.mem.split(' / ')[0] }}</span>
              <span>Mạng {{ s.container.stats.net }}</span>
            </div>
            <div v-if="s.container?.ports.length" class="mt-1 flex flex-wrap gap-2 text-xs">
              <a
                v-for="p in s.container.ports" :key="p.published" :href="`http://localhost:${p.published}`" target="_blank" rel="noopener"
                class="text-(--ui-primary)" @click.stop
              >:{{ p.published }} → {{ p.target }} ↗</a>
            </div>
            <div class="mt-2 flex items-center gap-1" @click.stop>
              <template v-if="isAdmin && data.docker.available">
                <UButton
                  v-if="s.container?.state !== 'running'" size="xs" icon="i-lucide-play" label="Bật"
                  :loading="busy === 'up' + s.name" :disabled="actionRunning" @click="act('up', s.name)"
                />
                <template v-else>
                  <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-square" label="Dừng" :loading="busy === 'stop' + s.name" :disabled="actionRunning" @click="act('stop', s.name)" />
                  <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-rotate-cw" aria-label="Chạy lại" :loading="busy === 'restart' + s.name" :disabled="actionRunning" @click="act('restart', s.name)" />
                </template>
              </template>
              <UButton
                v-if="s.container" size="xs" :color="stateOf(s).color === 'error' ? 'error' : 'neutral'" :variant="stateOf(s).color === 'error' ? 'soft' : 'ghost'"
                icon="i-lucide-bot" label="Hỏi agent" class="ms-auto" @click="askAgent(s)"
              />
            </div>
          </div>
        </div>

        <LogTerminal
          :url="logUrl"
          :title="view?.kind === 'action' ? 'Thao tác docker compose' : view?.name ?? ''"
          :subtitle="view?.kind === 'action' ? (data.action ? `trạng thái: ${data.action.status}` : '') : 'log container'"
          :empty="view?.kind === 'action' ? 'Chưa chạy thao tác nào.' : 'Chưa có log (container chưa chạy?).'"
        />
      </div>
    </template>
  </div>
</template>
