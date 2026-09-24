<script setup lang="ts">
const { user, isAdmin } = useAuth()

const { data: health } = await useFetch<{ status: string, version: string }>('/api/health')
const { data: prov } = await useFetch<{ providers: Provider[] }>('/api/providers')
const { data: proj } = await useFetch<{ projects: Project[] }>('/api/projects')
const { data: tpl } = await useFetch<{ templates: OrgModel[] }>('/api/templates')

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
          <p class="text-sm text-(--ui-text-muted)">Mô hình mẫu</p>
          <p class="text-2xl font-semibold">{{ tpl?.templates.length ?? 0 }}</p>
        </UCard>
      </div>
    </div>
  </PageShell>
</template>
