<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import AppTopBar from './components/AppTopBar.vue';

const route = useRoute();

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
    <a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-raised focus:px-3 focus:py-1.5">{{ $t('common.skipToContent') }}</a>
    <AppTopBar v-if="route.meta['shell']" :attention="0" />
    <div id="main" class="flex min-h-0 flex-1 flex-col overflow-y-auto" :class="{ 'overflow-hidden': route.meta['shell'] }">
      <RouterView />
    </div>
  </div>
</template>
