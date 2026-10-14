<script setup lang="ts">
// One mark per enabled AI connection in the header (desktop): its vendor's logo
// (else its initial) in a ring. The ring fills to the most-used usage window
// (5 hours, the week); red when the connection is in error, a plain green ring
// when it reports no window.
import type { MessageKey } from '~/locales/vi'

const { t } = useLang()
const { data } = useLiveFetch<{ providers: Provider[] }>('/api/providers', { lazy: true })
const { limits, refresh } = useLimits()
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  refresh()
  timer = setInterval(() => refresh(), 60000)
})
onBeforeUnmount(() => clearInterval(timer))

// the vendor's logo where there is one (simple-icons); written in full so the
// icon bundle picks them up
const kindLogo: Record<string, string> = {
  claude_cli: 'i-simple-icons-claude', anthropic: 'i-simple-icons-claude',
  codex_cli: 'i-simple-icons-openai', openai: 'i-simple-icons-openai',
  gemini_cli: 'i-simple-icons-googlegemini', antigravity_cli: 'i-simple-icons-google'
}
const presetLogo: Record<string, string> = {
  openrouter: 'i-simple-icons-openrouter', gemini: 'i-simple-icons-googlegemini', deepseek: 'i-simple-icons-deepseek',
  xai: 'i-simple-icons-x', mistral: 'i-simple-icons-mistralai', perplexity: 'i-simple-icons-perplexity',
  nvidia: 'i-simple-icons-nvidia', qwen: 'i-simple-icons-qwen', minimax: 'i-simple-icons-minimax',
  ollama: 'i-simple-icons-ollama', lmstudio: 'i-simple-icons-lmstudio'
}
// brand colours of the logos that have one; the rest take the text colour
const logoColor: Record<string, string> = {
  'i-simple-icons-claude': 'text-[#D97757]', 'i-simple-icons-googlegemini': 'text-[#8E75B2]', 'i-simple-icons-google': 'text-[#4285F4]',
  'i-simple-icons-deepseek': 'text-[#4D6BFE]', 'i-simple-icons-mistralai': 'text-[#FA520F]', 'i-simple-icons-nvidia': 'text-[#76B900]',
  'i-simple-icons-perplexity': 'text-[#1FB8CD]', 'i-simple-icons-qwen': 'text-[#615CED]'
}
const palette = ['bg-violet-500', 'bg-sky-500', 'bg-emerald-500', 'bg-amber-500', 'bg-rose-500', 'bg-indigo-500', 'bg-teal-500', 'bg-orange-500']
const colorOf = (key: string) => palette[[...key].reduce((a, c) => a + c.charCodeAt(0), 0) % palette.length]
const R = 10
const C = 2 * Math.PI * R

const rings = computed(() => (data.value?.providers ?? []).filter(p => p.enabled).map((p) => {
  const ws = Object.entries(limits.value.find(l => l.provider_id === p.id)?.windows ?? {})
    .filter(([k]) => k === 'five_hour' || k === 'seven_day')
  const used = ws.length ? Math.max(...ws.map(([, w]) => w.utilization)) : null
  const error = p.status === 'error'
  const stroke = error ? 'stroke-(--ui-error)' : used === null ? 'stroke-(--ui-success)' : used >= 0.9 ? 'stroke-(--ui-error)' : used >= 0.7 ? 'stroke-(--ui-warning)' : 'stroke-(--ui-primary)'
  const parts = [p.name]
  if (error) parts.push(p.status_detail || t('prov.statusError'))
  for (const [k, w] of ws) parts.push(`${t(`limits.window.${k}` as MessageKey)} ${Math.round(w.utilization * 100)}%`)
  const logo = presetLogo[p.preset] ?? kindLogo[p.kind] ?? ''
  return {
    id: p.id,
    logo,
    logoColor: logoColor[logo] ?? 'text-(--ui-text)',
    letter: p.name.slice(0, 1).toUpperCase(),
    bg: colorOf(p.kind),
    stroke,
    // error or no window: a full ring; else the used share of it
    dash: error || used === null ? C : Math.min(1, used) * C,
    title: parts.join(' · ')
  }
}))
</script>

<template>
  <div v-if="rings.length" class="hidden items-center gap-1 sm:flex">
    <NuxtLink
      v-for="r in rings" :key="r.id" to="/providers" :title="r.title" :aria-label="r.title"
      class="relative grid size-7 place-items-center rounded-full transition hover:bg-(--ui-bg-elevated)"
    >
      <svg viewBox="0 0 24 24" class="absolute inset-0 size-full -rotate-90">
        <circle cx="12" cy="12" :r="R" fill="none" stroke-width="2" class="stroke-(--ui-bg-accented)" />
        <circle
          cx="12" cy="12" :r="R" fill="none" stroke-width="2" stroke-linecap="round" :class="r.stroke"
          :stroke-dasharray="`${r.dash} ${C}`" class="transition-all"
        />
      </svg>
      <UIcon v-if="r.logo" :name="r.logo" class="size-3.5" :class="r.logoColor" />
      <span v-else class="grid size-4.5 place-items-center rounded-full text-[10px] font-semibold text-white" :class="r.bg">{{ r.letter }}</span>
    </NuxtLink>
  </div>
</template>
