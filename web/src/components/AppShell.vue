<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue';
import { useRouter } from 'vue-router';
import { provideAssistantStore } from '../composables/assistantStore';
import { useApi } from '../composables/useApi';
import { languageEpoch } from '../i18n';
import { isNewConversationShortcut } from '../lib/newConversation';
import AppSidebar from './AppSidebar.vue';
import StatusBanner from './StatusBanner.vue';

// One stream of conversations for every screen: moving between them neither reopens it nor loses the sidebar.
const store = provideAssistantStore(useApi());
const router = useRouter();

// The core names days, statuses and notices in its language: read them again after it switches.
watch(languageEpoch, () => void store.refresh());

// Cmd+N (Ctrl+N) opens the composer of a new conversation from any screen of the shell. In the macOS app the menu
// bar may take the shortcut first; it then sends 'pouple:new-conversation', which main.ts turns into the same route.
function onKeydown(event: KeyboardEvent): void {
  if (event.defaultPrevented || event.repeat || !isNewConversationShortcut(event)) return;
  event.preventDefault();
  void router.push({ name: 'new' });
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
  </div>
</template>
