<script setup lang="ts">
import { useRoute } from 'vue-router';
import AppTopBar from './components/AppTopBar.vue';

const route = useRoute();
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-raised focus:px-3 focus:py-1.5">Skip to content</a>
    <AppTopBar v-if="route.meta['shell']" :attention="0" />
    <div id="main" class="flex min-h-0 flex-1 flex-col overflow-y-auto" :class="{ 'overflow-hidden': route.meta['shell'] }">
      <RouterView />
    </div>
  </div>
</template>
