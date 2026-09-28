<script setup lang="ts">
// Proposals waiting for a person, from anywhere (the CLI's have no chat).
import type { ProposedAction } from '~/components/ActionCard.vue'
const { t } = useLang()
const { isAdmin } = useAuth()
const { data, refresh } = useFetch<{ actions: (ProposedAction & { project_id?: string })[] }>('/api/actions/pending', { lazy: true, immediate: isAdmin.value, server: false })
const list = computed(() => data.value?.actions ?? [])
</script>

<template>
  <div v-if="isAdmin && list.length" class="space-y-2">
    <p class="flex items-center gap-1.5 text-sm font-medium">
      <UIcon name="i-lucide-inbox" class="size-4" />{{ t('inbox.title', { n: list.length }) }}
      <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-refresh-cw" :aria-label="t('inbox.refresh')" @click="refresh()" />
    </p>
    <ActionCard v-for="a in list" :key="a.id" :action="a" @updated="refresh()" />
  </div>
</template>
