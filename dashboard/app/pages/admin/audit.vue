<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'

definePageMeta({ admin: true })

interface AuditEntry {
  id: string
  actor: string
  action: string
  target: string
  detail: Record<string, unknown>
  at: string
}

const { data, refresh, status } = await useFetch<{ entries: AuditEntry[] }>('/api/audit', { query: { limit: 200 } })

const columns: TableColumn<AuditEntry>[] = [
  { accessorKey: 'at', header: 'Thời gian' },
  { accessorKey: 'actor', header: 'Người thực hiện' },
  { accessorKey: 'action', header: 'Hành động' },
  { accessorKey: 'target', header: 'Đối tượng' },
  { accessorKey: 'detail', header: 'Chi tiết' }
]

const failed = (a: string) => a.includes('failed') || a.includes('throttled')
</script>

<template>
  <PageShell title="Audit log">
    <template #actions>
      <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="status === 'pending'" @click="refresh()" />
    </template>

    <UTable :data="data?.entries ?? []" :columns="columns" :loading="status === 'pending'">
      <template #at-cell="{ row }">
        {{ new Date(row.original.at).toLocaleString('vi-VN') }}
      </template>
      <template #action-cell="{ row }">
        <UBadge :label="row.original.action" :color="failed(row.original.action) ? 'error' : 'neutral'" variant="subtle" />
      </template>
      <template #detail-cell="{ row }">
        <code class="text-xs text-(--ui-text-muted)">{{ JSON.stringify(row.original.detail) }}</code>
      </template>
    </UTable>
  </PageShell>
</template>
