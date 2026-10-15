<script setup lang="ts">
// One mark per enabled AI connection in the header (desktop): its logo in its
// usage rings (useProviderRings).
const { rings, refresh } = useProviderRings()
// the windows come from the vendors, not from office: asked again each minute
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  refresh()
  timer = setInterval(() => refresh(), 60000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div v-if="rings.length" class="hidden items-center gap-1 sm:flex">
    <NuxtLink
      v-for="r in rings" :key="r.id" to="/providers" :title="r.title" :aria-label="r.title"
      class="grid size-7 place-items-center rounded-full transition hover:bg-(--ui-bg-elevated)"
    >
      <ProviderRing :ring="r" class="size-full" />
    </NuxtLink>
  </div>
</template>
