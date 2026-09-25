<script setup lang="ts">
// Rebuild office from its source and restart on the new build; the supervisor
// (office run) rolls back to the previous build if the new one fails.
interface UpdateState { status: 'idle' | 'running' | 'failed' | 'restarting', step?: string, error?: string, started_at?: string, finished_at?: string }
interface Status {
  supervised: boolean
  build: { version: string, revision?: string, time?: string, dirty: boolean }
  last: { state: 'ok' | 'rolled_back' | 'failed', message: string, at: string } | null
  busy: { chats: number, tasks: number }
  source?: { root: string, ui_dir: string }
  state?: UpdateState
}

const toast = useToast()
const { data, refresh } = await useFetch<Status>('/api/system/update')
const runTests = ref(true)
const starting = ref(false)
const restarting = ref(false)
const state = computed(() => data.value?.state)
const running = computed(() => state.value?.status === 'running')
const showLog = ref(false)
const streamKey = ref(0) // new stream per update

let poll: ReturnType<typeof setInterval> | undefined
function watchUpdate() {
  clearInterval(poll)
  poll = setInterval(async () => {
    try {
      await refresh()
      if (state.value?.status === 'restarting') waitForRestart()
      if (state.value?.status !== 'running') clearInterval(poll)
    } catch { waitForRestart() } // the server went away: it is restarting
  }, 1500)
}
onMounted(() => { if (running.value) { showLog.value = true; watchUpdate() } })
onBeforeUnmount(() => clearInterval(poll))

async function start(force = false) {
  starting.value = true
  try {
    await $fetch('/api/system/update', { method: 'POST', body: { test: runTests.value, force } })
    showLog.value = true
    streamKey.value++
    await refresh()
    watchUpdate()
  } catch (e) {
    const d = (e as { data?: { code?: string, error?: string } }).data
    if (d?.code === 'busy' && confirm(`${d.error}. Vẫn cập nhật?`)) {
      starting.value = false
      return start(true)
    }
    toast.add({ title: apiError(e), color: 'error' })
  } finally {
    starting.value = false
  }
}

// the server restarts: wait until it answers again, then reload the page
function waitForRestart() {
  if (restarting.value) return
  restarting.value = true
  clearInterval(poll)
  const started = Date.now()
  const t = setInterval(async () => {
    try {
      const h = await $fetch<{ status: string }>('/api/health')
      if (h.status === 'ok' && Date.now() - started > 3000) {
        clearInterval(t)
        window.location.reload()
      }
    } catch { /* still restarting */ }
  }, 1500)
}

const lastMeta = {
  ok: { color: 'success' as const, icon: 'i-lucide-circle-check', title: 'Lần cập nhật gần nhất thành công' },
  rolled_back: { color: 'warning' as const, icon: 'i-lucide-undo-2', title: 'Bản mới lỗi, đã quay về bản trước' },
  failed: { color: 'error' as const, icon: 'i-lucide-circle-x', title: 'Lần cập nhật gần nhất không thành công' }
}
const when = (d: string) => new Date(d).toLocaleString('vi-VN')
</script>

<template>
  <PageShell title="Cập nhật office">
    <div v-if="data" class="mx-auto max-w-3xl space-y-4">
      <UCard>
        <div class="flex flex-wrap items-start gap-4">
          <UIcon name="i-lucide-package" class="mt-0.5 size-6 text-primary" />
          <div class="min-w-0 flex-1 space-y-1 text-sm">
            <p class="font-semibold">
              agent-office {{ data.build.version }}
              <code v-if="data.build.revision" class="ms-1 text-xs text-(--ui-text-muted)">{{ data.build.revision.slice(0, 7) }}</code>
              <UBadge v-if="data.build.dirty" color="warning" variant="subtle" size="sm" label="có thay đổi chưa commit" class="ms-1" />
            </p>
            <p v-if="data.build.time" class="text-xs text-(--ui-text-muted)">Commit lúc {{ when(data.build.time) }}</p>
            <p v-if="data.source" class="truncate font-mono text-xs text-(--ui-text-muted)">Mã nguồn: {{ data.source.root }}</p>
          </div>
          <UBadge :color="data.supervised ? 'success' : 'neutral'" variant="subtle" :icon="data.supervised ? 'i-lucide-shield-check' : 'i-lucide-shield-off'" :label="data.supervised ? 'Có supervisor' : 'Không có supervisor'" />
        </div>
      </UCard>

      <UAlert
        v-if="data.last && data.last.state !== 'ok' || data.last?.state === 'ok' && !running && !restarting" :color="lastMeta[data.last!.state].color" variant="subtle"
        :icon="lastMeta[data.last!.state].icon" :title="lastMeta[data.last!.state].title" :description="`${data.last!.message} · ${when(data.last!.at)}`"
      />

      <UAlert
        v-if="!data.supervised" color="neutral" variant="subtle" icon="i-lucide-info" title="Office đang chạy không có supervisor"
        description="Tự cập nhật cần supervisor để khởi động lại và quay về bản cũ khi lỗi. Tắt office rồi chạy `office run` (hoặc `make start`), không dùng `office serve`."
      />
      <UAlert
        v-else-if="!data.source" color="neutral" variant="subtle" icon="i-lucide-info" title="Bản cài sẵn, không có mã nguồn"
        description="Máy này chạy file office đã build sẵn. Cập nhật từ bản phát hành (release) sẽ có sau."
      />

      <UCard v-else>
        <div class="space-y-4">
          <div>
            <p class="font-semibold">Cập nhật từ mã nguồn</p>
            <p class="text-sm text-(--ui-text-muted)">
              Build server và dashboard từ mã nguồn hiện tại ra bản mới, rồi khởi động lại. Build hoặc test lỗi thì bản đang chạy giữ nguyên;
              bản mới không lên được thì supervisor tự quay về bản trước.
            </p>
          </div>
          <UCheckbox v-model="runTests" label="Chạy test trước (chậm hơn, an toàn hơn)" :disabled="running" />
          <p v-if="data.busy.chats || data.busy.tasks" class="text-sm text-(--ui-warning)">
            <UIcon name="i-lucide-triangle-alert" class="align-middle" />
            Đang có {{ data.busy.chats }} lượt chat và {{ data.busy.tasks }} Việc chạy. Khởi động lại sẽ cắt ngang chúng.
          </p>
          <p class="text-xs text-(--ui-text-muted)">Tiến trình office chạy cho project (vd pnpm dev) sẽ dừng; mục có "bật cùng office" tự chạy lại. Container không bị ảnh hưởng.</p>
          <div class="flex items-center gap-3">
            <UButton icon="i-lucide-refresh-cw" label="Cập nhật và khởi động lại" :loading="starting || running" :disabled="restarting" @click="start()" />
            <span v-if="running" class="text-sm text-(--ui-text-muted)">{{ state?.step }}…</span>
            <span v-else-if="state?.status === 'failed'" class="text-sm text-(--ui-error)">{{ state.error }}</span>
          </div>
        </div>
      </UCard>

      <div v-if="restarting" class="flex items-center gap-2 rounded-lg border border-(--ui-border) p-4 text-sm">
        <UIcon name="i-lucide-loader-circle" class="size-5 animate-spin text-primary" />
        Đang khởi động lại bằng bản mới… trang sẽ tự tải lại.
      </div>

      <LogTerminal v-if="showLog && data.source" :key="streamKey" url="/api/system/update/stream" title="Cập nhật office" :subtitle="state?.step" empty="Đang bắt đầu…" />
    </div>
  </PageShell>
</template>
