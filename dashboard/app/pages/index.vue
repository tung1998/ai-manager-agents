<script setup lang="ts">
const { user, isAdmin } = useAuth()

const { data: health } = await useFetch<{ status: string, version: string }>('/api/health')
const { data: prov } = await useFetch<{ providers: Provider[] }>('/api/providers')
const { data: proj } = await useFetch<{ projects: Project[] }>('/api/projects')
const { data: tpl } = await useFetch<{ templates: OrgModel[] }>('/api/templates')

interface Job { id: string, project_id: string, title: string, goal: string, pending_patches?: number, status: 'running' | 'done' | 'failed' | 'cancelled' | 'rejected' | 'needs_input', cost_usd: number, created_at: string }
const { data: jobData } = await useFetch<{ tasks: Job[], projects: Record<string, string> }>('/api/jobs')
const recentJobs = computed(() => (jobData.value?.tasks ?? []).slice(0, 6))
const jobStatus: Record<Job['status'], { label: string, color: 'info' | 'success' | 'error' | 'neutral' | 'warning', icon: string }> = {
  running: { label: 'Đang chạy', color: 'info', icon: 'i-lucide-loader' },
  done: { label: 'Xong', color: 'success', icon: 'i-lucide-circle-check' },
  failed: { label: 'Không thành công', color: 'error', icon: 'i-lucide-circle-x' },
  cancelled: { label: 'Đã dừng', color: 'neutral', icon: 'i-lucide-circle-slash' },
  rejected: { label: 'Không thông qua', color: 'warning', icon: 'i-lucide-thumbs-down' },
  needs_input: { label: 'Chờ bạn trả lời', color: 'warning', icon: 'i-lucide-message-circle-question' }
}
const when = (d: string) => new Date(d).toLocaleString('vi-VN', { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit' })

// health checks across projects
const { data: monData } = await useFetch<{ monitors: { id: string, name: string, project_id: string, status: string, last_message: string }[], summary: Record<string, number> }>('/api/monitors')
const downMonitors = computed(() => (monData.value?.monitors ?? []).filter(m => m.status === 'down'))

const providers = computed(() => prov.value?.providers ?? [])
const projects = computed(() => proj.value?.projects ?? [])
const okProviders = computed(() => providers.value.filter(p => p.status === 'ok').length)
const withModel = computed(() => projects.value.filter(p => p.model).length)

const steps = computed(() => [
  {
    done: okProviders.value > 0,
    title: 'Kết nối AI',
    text: okProviders.value ? `${okProviders.value}/${providers.value.length} kết nối hoạt động` : 'Thêm Claude, GPT hoặc CLI trên máy',
    to: '/providers', icon: 'i-lucide-plug'
  },
  {
    done: projects.value.length > 0,
    title: 'Thêm project',
    text: projects.value.length ? `${projects.value.length} project đang quản lý` : 'Chọn thư mục trên máy, hoặc tạo helper toàn máy',
    to: '/projects', icon: 'i-lucide-folder-git-2'
  },
  {
    done: projects.value.length > 0 && withModel.value === projects.value.length,
    title: 'Chọn mô hình cho project',
    text: projects.value.length ? `${withModel.value}/${projects.value.length} project đã có mô hình` : 'Solo, Team hoặc Tam quyền phân lập',
    to: projects.value.length ? '/projects' : '/templates', icon: 'i-lucide-network'
  }
])
</script>

<template>
  <PageShell title="Tổng quan">
    <div class="space-y-6">
      <UCard>
        <div class="flex flex-wrap items-center justify-between gap-4">
          <div>
            <p class="text-sm text-(--ui-text-muted)">Xin chào</p>
            <p class="text-lg font-semibold">{{ user?.name || user?.email }}</p>
          </div>
          <div class="flex items-center gap-2">
            <UBadge :label="user?.role" :color="isAdmin ? 'primary' : 'neutral'" variant="subtle" />
            <UBadge v-if="health?.status === 'ok'" :label="`API ${health.version}`" color="success" variant="subtle" icon="i-lucide-plug" />
            <UBadge v-else label="Mất kết nối API" color="error" variant="subtle" icon="i-lucide-plug-zap" />
          </div>
        </div>
      </UCard>

      <UCard>
        <template #header>
          <p class="font-medium">Thiết lập</p>
          <p class="text-sm text-(--ui-text-muted)">Thứ tự quản lý: project → mô hình → agent. Mỗi agent chạy trên một kết nối AI.</p>
        </template>
        <ol class="space-y-3">
          <li v-for="(s, i) in steps" :key="s.title">
            <NuxtLink :to="s.to" class="flex items-center gap-3 rounded-md p-2 transition hover:bg-(--ui-bg-muted)">
              <span
                class="flex size-7 shrink-0 items-center justify-center rounded-full text-sm font-medium"
                :class="s.done ? 'bg-(--ui-success) text-white' : 'bg-(--ui-bg-accented)'"
              >
                <UIcon v-if="s.done" name="i-lucide-check" class="size-4" />
                <span v-else>{{ i + 1 }}</span>
              </span>
              <UIcon :name="s.icon" class="size-5 text-primary" />
              <div class="min-w-0 flex-1">
                <p class="font-medium">{{ s.title }}</p>
                <p class="text-sm text-(--ui-text-muted)">{{ s.text }}</p>
              </div>
              <UIcon name="i-lucide-chevron-right" class="size-4 text-(--ui-text-dimmed)" />
            </NuxtLink>
          </li>
        </ol>
      </UCard>

      <div class="grid gap-4 sm:grid-cols-3">
        <UCard>
          <p class="text-sm text-(--ui-text-muted)">Project</p>
          <p class="text-2xl font-semibold">{{ projects.length }}</p>
        </UCard>
        <UCard>
          <p class="text-sm text-(--ui-text-muted)">Kết nối AI</p>
          <p class="text-2xl font-semibold">{{ providers.length }}</p>
        </UCard>
        <UCard>
          <p class="text-sm text-(--ui-text-muted)">Mô hình</p>
          <p class="text-2xl font-semibold">{{ tpl?.templates.length ?? 0 }}</p>
        </UCard>
      </div>

      <UCard v-if="monData?.monitors.length">
        <div class="flex flex-wrap items-center gap-4">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-heart-pulse" class="size-5 text-primary" />
            <p class="font-semibold">Giám sát</p>
          </div>
          <p class="font-mono text-lg"><span class="text-(--ui-success)">{{ monData.summary.up }}</span> / {{ monData.monitors.length }} Up</p>
          <UBadge v-if="monData.summary.down" color="error" variant="subtle" :label="`${monData.summary.down} Down`" />
          <UBadge v-if="monData.summary.pending" color="neutral" variant="subtle" :label="`${monData.summary.pending} đang chờ`" />
        </div>
        <div v-if="downMonitors.length" class="mt-3 divide-y divide-(--ui-border) rounded-md border border-(--ui-border)">
          <NuxtLink
            v-for="m in downMonitors" :key="m.id" :to="`/projects/${m.project_id}?tab=ops&section=monitors`"
            class="flex items-center gap-2 px-3 py-2 text-sm hover:bg-(--ui-bg-elevated)"
          >
            <UBadge color="error" variant="subtle" size="sm" label="Down" />
            <span class="font-medium">{{ m.name }}</span>
            <span class="truncate text-(--ui-text-muted)">{{ m.last_message }}</span>
          </NuxtLink>
        </div>
      </UCard>

      <UCard v-if="recentJobs.length" :ui="{ body: 'p-0 sm:p-0' }">
        <template #header>
          <p class="font-semibold">Việc gần đây</p>
        </template>
        <div class="divide-y divide-(--ui-border)">
          <NuxtLink
            v-for="j in recentJobs" :key="j.id" :to="`/projects/${j.project_id}?tab=tasks&task=${j.id}`"
            class="flex items-center gap-3 px-4 py-2.5 transition hover:bg-(--ui-bg-elevated)"
          >
            <UIcon :name="jobStatus[j.status].icon" class="size-4 shrink-0" :class="{ 'animate-spin': j.status === 'running' }" />
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium">{{ j.title || j.goal }}</p>
              <p class="truncate text-xs text-(--ui-text-muted)">{{ jobData?.projects[j.project_id] ?? j.project_id }} · {{ when(j.created_at) }}</p>
            </div>
            <span class="text-xs tabular-nums text-(--ui-text-muted)">${{ j.cost_usd.toFixed(3) }}</span>
            <UBadge
              :color="j.status === 'done' && j.pending_patches ? 'warning' : jobStatus[j.status].color" variant="subtle" size="sm"
              :label="j.status === 'done' && j.pending_patches ? `Chờ duyệt (${j.pending_patches})` : jobStatus[j.status].label"
            />
          </NuxtLink>
        </div>
      </UCard>
    </div>
  </PageShell>
</template>
