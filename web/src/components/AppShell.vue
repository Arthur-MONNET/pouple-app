<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { provideAssistantStore } from '../composables/assistantStore';
import { useApi } from '../composables/useApi';
import { languageEpoch } from '../i18n';
import { useShortcuts } from '../composables/useShortcuts';
import { matches } from '../lib/shortcuts';
import AppSidebar from './AppSidebar.vue';
import CommandPalette from './CommandPalette.vue';
import StatusBanner from './StatusBanner.vue';
import UndoToast from './UndoToast.vue';

// One stream of conversations for every screen: moving between them neither reopens it nor loses the sidebar.
const store = provideAssistantStore(useApi());
const router = useRouter();
const route = useRoute();
const { shortcuts } = useShortcuts();
const paletteOpen = ref(false);
// Going anywhere, by the palette or by the Cmd+N shortcut under it, ends it.
watch(() => route.fullPath, () => (paletteOpen.value = false));

// The core names days, statuses and notices in its language: read them again after it switches.
watch(languageEpoch, () => void store.refresh());

// The shortcuts are the user's (Cmd+K and Cmd+N by default). In the macOS app the menu bar may take Cmd+N first; it then
// sends 'pouple:new-conversation', which main.ts turns into the same route.
function onKeydown(event: KeyboardEvent): void {
  if (event.defaultPrevented || event.repeat) return;
  if (matches(event, shortcuts.value.palette)) {
    event.preventDefault();
    paletteOpen.value = !paletteOpen.value;
  } else if (matches(event, shortcuts.value['new-conversation'])) {
    event.preventDefault();
    void router.push({ name: 'new' });
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown));
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown));
</script>

<template>
  <div class="relative flex min-h-0 flex-1">
    <a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-raised focus:px-3 focus:py-1.5">{{ $t('common.skipToContent') }}</a>
    <AppSidebar />
    <div id="main" class="flex min-w-0 flex-1 flex-col bg-canvas">
      <StatusBanner :status="store.status.value" :error="store.error.value" />
      <RouterView />
    </div>
    <UndoToast />
    <CommandPalette v-if="paletteOpen" @close="paletteOpen = false" />
  </div>
</template>
