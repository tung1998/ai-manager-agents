<script setup lang="ts">
interface UsageRow { key: string, label: string, runs: number, input_tokens: number, output_tokens: number, cost_usd: number, unknown_cost: number }
interface Price { input: number, output: number, cache_read?: number, cache_write?: number }
interface Settings { daily_limit_usd: number, project_limits?: Record<string, number>, prices?: Record<string, Price>, warn_ratio?: number }
interface Summary {
  today: number
  daily_limit: number
  warn_ratio: number
  period: number
  days: number
  by_day: UsageRow[]
  by_project: UsageRow[]
  by_model: UsageRow[]
  project_today: Record<string, number>
  settings: Settings
  default_prices: Record<string, Price>
}
interface Run {
  id: string
  kind: string
  project_name: string
  provider_name: string
  model: string
  status: 'ok' | 'error' | 'blocked'
  input_tokens: number
  output_tokens: number
  cost_usd: number | null
  cost_source: 'provider' | 'estimate' | 'unknown'
  duration_ms: number
  error: string
  actor: string
  created_at: string
}

const { isAdmin } = useAuth()
const toast = useToast()
const days = ref(30)
const { data: sum, refresh } = await useFetch<Summary>('/api/usage/summary', { query: { days } })
const { data: runsData, refresh: refreshRuns } = await useFetch<{ runs: Run[] }>('/api/usage/runs', { query: { days, limit: 100 } })
const { data: projData } = await useFetch<{ projects: Project[] }>('/api/projects')

const usd = (v: number) => v >= 100 ? `$${v.toFixed(0)}` : v >= 1 ? `$${v.toFixed(2)}` : `$${v.toFixed(3)}`
const tokens = (v: number) => v >= 1e6 ? `${(v / 1e6).toFixed(1)}M` : v >= 1e3 ? `${(v / 1e3).toFixed(1)}K` : `${v}`

// ---- budget status (status colors always paired with icon + label) ----
const budget = computed(() => {
  const s = sum.value
  if (!s || !s.daily_limit) return { ratio: 0, state: 'none' as const }
  const ratio = s.today / s.daily_limit
  return { ratio, state: ratio >= 1 ? 'over' as const : ratio >= s.warn_ratio ? 'warn' as const : 'ok' as const }
})
const budgetMeta = {
  none: { label: 'Chưa đặt trần', icon: 'i-lucide-infinity', color: 'neutral' as const, iconClass: 'text-(--ui-text-muted)' },
  ok: { label: 'Trong ngân sách', icon: 'i-lucide-circle-check', color: 'success' as const, iconClass: 'text-(--ui-success)' },
  warn: { label: 'Sắp chạm trần', icon: 'i-lucide-triangle-alert', color: 'warning' as const, iconClass: 'text-(--ui-warning)' },
  over: { label: 'Đã chạm trần: tạm dừng gọi AI', icon: 'i-lucide-octagon-x', color: 'error' as const, iconClass: 'text-(--ui-error)' }
}

const totalRuns = computed(() => sum.value?.by_day.reduce((a, d) => a + d.runs, 0) ?? 0)
const unknownRuns = computed(() => sum.value?.by_day.reduce((a, d) => a + d.unknown_cost, 0) ?? 0)

// ---- daily chart: one series, one hue, bars anchored to the baseline ----
const maxDay = computed(() => Math.max(0.0001, ...(sum.value?.by_day.map(d => d.cost_usd) ?? [0])))
const hovered = ref<UsageRow | null>(null)
const showTable = ref(false)
const dayLabel = (key: string) => new Date(key + 'T00:00:00').toLocaleDateString('vi-VN', { day: '2-digit', month: '2-digit' })
const tickEvery = computed(() => Math.max(1, Math.ceil((sum.value?.by_day.length ?? 30) / 8)))

const maxProject = computed(() => Math.max(0.0001, ...(sum.value?.by_project.map(r => r.cost_usd) ?? [0])))
const maxModel = computed(() => Math.max(0.0001, ...(sum.value?.by_model.map(r => r.cost_usd) ?? [0])))

watch(days, () => { refresh(); refreshRuns() })

// ---- settings ----
const settingsOpen = ref(false)
const form = reactive({ daily: 0, projects: {} as Record<string, number>, prices: [] as { model: string, input: number, output: number }[] })
function openSettings() {
  const s = sum.value!.settings
  form.daily = s.daily_limit_usd || 0
  form.projects = { ...(s.project_limits ?? {}) }
  form.prices = Object.entries(s.prices ?? {}).map(([model, p]) => ({ model, input: p.input, output: p.output }))
  settingsOpen.value = true
}
async function saveSettings() {
  try {
    const prices: Record<string, Price> = {}
    for (const p of form.prices) {
      if (p.model.trim()) prices[p.model.trim()] = { input: Number(p.input) || 0, output: Number(p.output) || 0 }
    }
    const project_limits: Record<string, number> = {}
    for (const [id, v] of Object.entries(form.projects)) {
      if (Number(v) > 0) project_limits[id] = Number(v)
    }
    await $fetch('/api/usage/settings', { method: 'PUT', body: { daily_limit_usd: Number(form.daily) || 0, project_limits, prices } })
    settingsOpen.value = false
    await refresh()
    toast.add({ title: 'Đã lưu ngân sách', color: 'success' })
  } catch (e) {
    toast.add({ title: apiError(e), color: 'error' })
  }
}

const kindLabel: Record<string, string> = { provider_test: 'Gửi thử', setup_propose: 'Thiết lập bằng AI', chat: 'Chat' }
const statusMeta: Record<string, { label: string, color: 'success' | 'error' | 'warning' }> = {
  ok: { label: 'OK', color: 'success' }, error: { label: 'Lỗi', color: 'error' }, blocked: { label: 'Bị chặn', color: 'warning' }
}
</script>

<template>
  <PageShell title="Chi phí">
    <template #actions>
      <USelect v-model="days" :items="[{ label: '7 ngày', value: 7 }, { label: '30 ngày', value: 30 }, { label: '90 ngày', value: 90 }]" size="sm" class="w-28" />
      <UButton v-if="isAdmin" icon="i-lucide-settings-2" label="Ngân sách" color="neutral" variant="outline" @click="openSettings" />
    </template>

    <div v-if="sum" class="space-y-6">
      <!-- tiles -->
      <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <UCard>
          <p class="text-sm text-(--ui-text-muted)">Hôm nay</p>
          <p class="mt-1 text-2xl font-semibold tabular-nums">
            {{ usd(sum.today) }}<span v-if="sum.daily_limit" class="text-base font-normal text-(--ui-text-muted)"> / {{ usd(sum.daily_limit) }}</span>
          </p>
          <UProgress v-if="sum.daily_limit" :model-value="Math.min(100, budget.ratio * 100)" :color="budgetMeta[budget.state].color" size="sm" class="mt-3" />
          <div class="mt-2 flex items-center gap-1.5 text-xs">
            <UIcon :name="budgetMeta[budget.state].icon" class="size-3.5" :class="budgetMeta[budget.state].iconClass" />
            <span class="text-(--ui-text-muted)">{{ budgetMeta[budget.state].label }}</span>
          </div>
        </UCard>
        <UCard>
          <p class="text-sm text-(--ui-text-muted)">{{ sum.days }} ngày qua</p>
          <p class="mt-1 text-2xl font-semibold tabular-nums">{{ usd(sum.period) }}</p>
          <p class="mt-2 text-xs text-(--ui-text-muted)">TB {{ usd(sum.period / sum.days) }}/ngày</p>
        </UCard>
        <UCard>
          <p class="text-sm text-(--ui-text-muted)">Lượt gọi AI</p>
          <p class="mt-1 text-2xl font-semibold tabular-nums">{{ totalRuns }}</p>
          <p class="mt-2 text-xs text-(--ui-text-muted)">trong {{ sum.days }} ngày</p>
        </UCard>
        <UCard>
          <p class="text-sm text-(--ui-text-muted)">Chưa rõ chi phí</p>
          <p class="mt-1 text-2xl font-semibold tabular-nums">{{ unknownRuns }}</p>
          <p class="mt-2 text-xs text-(--ui-text-muted)">model chưa có giá, thêm trong Ngân sách</p>
        </UCard>
      </div>

      <!-- daily chart -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <p class="font-medium">Chi phí theo ngày</p>
            <UButton size="xs" color="neutral" variant="ghost" :label="showTable ? 'Xem biểu đồ' : 'Xem dạng bảng'" @click="showTable = !showTable" />
          </div>
        </template>
        <div v-if="!showTable" class="relative">
          <div class="flex h-48 items-end gap-0.5 border-b border-(--ui-border) pb-px" @mouseleave="hovered = null">
            <div
              v-for="d in sum.by_day" :key="d.key"
              class="flex h-full flex-1 cursor-default items-end"
              @mouseenter="hovered = d"
            >
              <div
                class="w-full rounded-t bg-(--ui-primary) transition-opacity"
                :class="hovered && hovered.key !== d.key ? 'opacity-40' : ''"
                :style="{ height: d.cost_usd ? `max(2px, ${(d.cost_usd / maxDay) * 100}%)` : '0' }"
              />
            </div>
          </div>
          <div class="mt-1 flex gap-0.5 text-[10px] text-(--ui-text-muted)">
            <span v-for="(d, i) in sum.by_day" :key="d.key" class="flex-1 text-center">{{ i % tickEvery === 0 ? dayLabel(d.key) : '' }}</span>
          </div>
          <p class="absolute left-0 top-0 text-[10px] text-(--ui-text-muted)">{{ usd(maxDay) }}</p>
          <div
            v-if="hovered" class="pointer-events-none absolute right-0 top-0 rounded-md border border-(--ui-border) bg-(--ui-bg) px-3 py-2 text-xs shadow-sm"
          >
            <p class="font-medium">{{ new Date(hovered.key + 'T00:00:00').toLocaleDateString('vi-VN') }}</p>
            <p class="tabular-nums">{{ usd(hovered.cost_usd) }} · {{ hovered.runs }} lượt</p>
            <p class="text-(--ui-text-muted) tabular-nums">{{ tokens(hovered.input_tokens) }} in / {{ tokens(hovered.output_tokens) }} out</p>
          </div>
        </div>
        <table v-else class="w-full text-sm">
          <thead class="text-left text-xs text-(--ui-text-muted)">
            <tr><th class="py-1">Ngày</th><th>Chi phí</th><th>Lượt</th><th>Token in / out</th></tr>
          </thead>
          <tbody>
            <tr v-for="d in [...sum.by_day].reverse()" :key="d.key" class="border-t border-(--ui-border)">
              <td class="py-1">{{ dayLabel(d.key) }}</td>
              <td class="tabular-nums">{{ usd(d.cost_usd) }}</td>
              <td class="tabular-nums">{{ d.runs }}</td>
              <td class="tabular-nums">{{ tokens(d.input_tokens) }} / {{ tokens(d.output_tokens) }}</td>
            </tr>
          </tbody>
        </table>
      </UCard>

      <!-- breakdowns -->
      <div class="grid gap-4 lg:grid-cols-2">
        <UCard v-for="block in [
          { title: 'Theo project', rows: sum.by_project, max: maxProject, empty: 'Không gắn project' },
          { title: 'Theo model', rows: sum.by_model, max: maxModel, empty: '—' }
        ]" :key="block.title">
          <template #header><p class="font-medium">{{ block.title }}</p></template>
          <p v-if="!block.rows?.length" class="text-sm text-(--ui-text-muted)">Chưa có lượt gọi nào.</p>
          <div class="space-y-2.5">
            <div v-for="r in block.rows" :key="r.key" class="text-sm">
              <div class="flex items-baseline justify-between gap-2">
                <span class="truncate">{{ block.title === 'Theo model' ? (r.key || '—') : (r.label || block.empty) }}</span>
                <span class="shrink-0 tabular-nums">{{ usd(r.cost_usd) }}</span>
              </div>
              <div class="mt-1 h-1.5 rounded-full bg-(--ui-bg-accented)">
                <div class="h-full rounded-full bg-(--ui-primary)" :style="{ width: `${Math.max(1, (r.cost_usd / block.max) * 100)}%` }" />
              </div>
              <p class="mt-0.5 text-xs text-(--ui-text-muted)">
                {{ r.runs }} lượt · {{ tokens(r.input_tokens) }} in / {{ tokens(r.output_tokens) }} out
                <template v-if="block.title === 'Theo model' && r.label"> · {{ r.label }}</template>
                <template v-if="r.unknown_cost"> · {{ r.unknown_cost }} chưa rõ giá</template>
              </p>
            </div>
          </div>
        </UCard>
      </div>

      <!-- recent runs -->
      <UCard>
        <template #header><p class="font-medium">Lượt gọi gần đây</p></template>
        <p v-if="!runsData?.runs.length" class="text-sm text-(--ui-text-muted)">Chưa có lượt gọi nào.</p>
        <div v-else class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead class="text-left text-xs text-(--ui-text-muted)">
              <tr>
                <th class="py-1.5 pe-3">Thời gian</th><th class="pe-3">Loại</th><th class="pe-3">Project</th><th class="pe-3">Model</th>
                <th class="pe-3 text-right">Token</th><th class="pe-3 text-right">Chi phí</th><th class="pe-3 text-right">Thời gian chạy</th><th>Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in runsData.runs" :key="r.id" class="border-t border-(--ui-border) align-top">
                <td class="whitespace-nowrap py-1.5 pe-3 text-(--ui-text-muted)">{{ new Date(r.created_at).toLocaleString('vi-VN') }}</td>
                <td class="pe-3">{{ kindLabel[r.kind] ?? r.kind }}</td>
                <td class="pe-3">{{ r.project_name || '—' }}</td>
                <td class="pe-3"><code class="text-xs">{{ r.model }}</code><p class="text-xs text-(--ui-text-muted)">{{ r.provider_name }}</p></td>
                <td class="whitespace-nowrap pe-3 text-right tabular-nums">{{ tokens(r.input_tokens) }} / {{ tokens(r.output_tokens) }}</td>
                <td class="whitespace-nowrap pe-3 text-right tabular-nums">
                  <template v-if="r.cost_usd !== null">{{ usd(r.cost_usd) }}<span v-if="r.cost_source === 'estimate'" class="text-(--ui-text-muted)" title="Ước tính theo bảng giá"> ~</span></template>
                  <span v-else class="text-(--ui-text-muted)">—</span>
                </td>
                <td class="whitespace-nowrap pe-3 text-right tabular-nums">{{ (r.duration_ms / 1000).toFixed(1) }}s</td>
                <td>
                  <UBadge :label="statusMeta[r.status]!.label" :color="statusMeta[r.status]!.color" variant="subtle" size="sm" />
                  <p v-if="r.error" class="mt-0.5 max-w-xs truncate text-xs text-(--ui-text-muted)" :title="r.error">{{ r.error }}</p>
                </td>
              </tr>
            </tbody>
          </table>
          <p class="mt-2 text-xs text-(--ui-text-muted)">~ là ước tính theo bảng giá; Claude Code tự báo chi phí thật.</p>
        </div>
      </UCard>
    </div>

    <UModal v-model:open="settingsOpen" title="Ngân sách" :ui="{ content: 'max-w-xl' }">
      <template #body>
        <form id="budget-form" class="space-y-5" @submit.prevent="saveSettings">
          <UFormField label="Trần chi phí mỗi ngày (USD)" help="0 là không giới hạn. Chạm trần thì mọi lượt gọi AI bị chặn đến hết ngày.">
            <UInputNumber v-model="form.daily" :min="0" :step="0.5" :format-options="{ minimumFractionDigits: 0, maximumFractionDigits: 2 }" />
          </UFormField>

          <div v-if="projData?.projects.length">
            <p class="text-sm font-medium">Trần theo project (USD/ngày)</p>
            <div class="mt-2 space-y-2">
              <div v-for="p in projData.projects" :key="p.id" class="flex items-center justify-between gap-3 text-sm">
                <span class="truncate">{{ p.name }}</span>
                <UInputNumber v-model="form.projects[p.id]" :min="0" :step="0.5" placeholder="không giới hạn" size="sm" class="w-36" />
              </div>
            </div>
          </div>

          <div>
            <p class="text-sm font-medium">Giá model (USD / 1 triệu token)</p>
            <p class="text-xs text-(--ui-text-muted)">Dùng để ước tính khi gọi qua API. Claude đã có giá sẵn; thêm model khác như GPT ở đây.</p>
            <div class="mt-2 space-y-2">
              <div v-for="(p, i) in form.prices" :key="i" class="flex items-center gap-2">
                <UInput v-model="p.model" placeholder="gpt-5" size="sm" class="flex-1 font-mono" />
                <UInputNumber v-model="p.input" :min="0" :step="0.1" size="sm" class="w-28" placeholder="input" />
                <UInputNumber v-model="p.output" :min="0" :step="0.1" size="sm" class="w-28" placeholder="output" />
                <UButton icon="i-lucide-x" size="xs" color="neutral" variant="ghost" @click="form.prices.splice(i, 1)" />
              </div>
              <UButton icon="i-lucide-plus" label="Thêm giá model" size="xs" color="neutral" variant="outline" @click="form.prices.push({ model: '', input: 0, output: 0 })" />
            </div>
            <details class="mt-2 text-xs text-(--ui-text-muted)">
              <summary class="cursor-pointer">Giá có sẵn</summary>
              <ul class="mt-1 space-y-0.5 font-mono">
                <li v-for="(p, m) in sum?.default_prices" :key="m">{{ m }}: ${{ p.input }} / ${{ p.output }}</li>
              </ul>
            </details>
          </div>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Hủy" @click="settingsOpen = false" />
          <UButton type="submit" form="budget-form" label="Lưu" />
        </div>
      </template>
    </UModal>
  </PageShell>
</template>
