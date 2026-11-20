<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue';

// A file dropped outside a drop zone would make the window open it and leave the app. Nothing outside the
// conversation takes files, so such a drop is simply ignored.
const ignoreStrayDrop = (event: DragEvent): void => event.preventDefault();
onMounted(() => {
  window.addEventListener('dragover', ignoreStrayDrop);
  window.addEventListener('drop', ignoreStrayDrop);
});
onBeforeUnmount(() => {
  window.removeEventListener('dragover', ignoreStrayDrop);
  window.removeEventListener('drop', ignoreStrayDrop);
});
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <RouterView />
  </div>
</template>
