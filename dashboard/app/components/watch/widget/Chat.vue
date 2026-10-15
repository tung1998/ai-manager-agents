<script setup lang="ts">
// A project's chat in a box (the reusable ChatPanel); the box's own buttons
// sit in the chat's header, so it keeps a single header.
const { projectId, paneId } = useWatchBox()
const props = defineProps<{ conversationId?: string }>()
const { setPane } = useWatchLayout()
</script>

<template>
  <ChatPanel
    :key="projectId" :project-id="projectId" :conversation-id="props.conversationId" compact pane class="h-full min-h-0"
    @current="(id) => setPane(paneId, { conversationId: id })"
  >
    <template #lead><slot name="lead" /></template>
    <template #actions><slot name="actions" /></template>
  </ChatPanel>
</template>
