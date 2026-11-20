<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useAssistantStore } from '../composables/assistantStore';
import { useConnectorsStore } from '../composables/useConnectorsStore';
import { useExtrasHint } from '../composables/useExtrasHint';
import { useSettings } from '../composables/useSettings';
import { useUpdate } from '../composables/useUpdate';
import BrandMark from './BrandMark.vue';
import ConversationList from './ConversationList.vue';
import SidebarLink from './SidebarLink.vue';
import SidebarUsage from './SidebarUsage.vue';

const route = useRoute();
const router = useRouter();
const store = useAssistantStore();
const connectors = useConnectorsStore();
const settings = useSettings();
const updates = useUpdate();
const hint = useExtrasHint();

// The hint needs to know which optional connectors exist, and the Memory link whether the memory is on.
onMounted(() => {
  void connectors.refreshConnectors();
  if (!settings.settings.value) void settings.load();
});

const onConversations = computed(() => route.name === 'home' || route.name === 'conversation');
const selectedId = computed(() => (route.name === 'conversation' && typeof route.params['id'] === 'string' ? route.params['id'] : null));
/** Off by default, and whoever does not want it never sees it (docs/PRODUCT.md, Memory). */
const memoryOn = computed(() => settings.settings.value?.memory === true);
const usage = computed(() => store.status.value?.usage ?? null);

const select = (id: string): void => void router.push({ name: 'conversation', params: { id } });
</script>

<template>
  <aside class="flex w-72 shrink-0 flex-col border-r border-line-soft bg-surface/40" :aria-label="$t('nav.sidebar')">
    <div class="flex h-12 shrink-0 items-center gap-2 px-3">
      <RouterLink to="/" class="rounded-md" :aria-label="$t('nav.goHome')"><BrandMark /></RouterLink>
      <RouterLink
        v-if="updates.available.value"
        :to="{ path: '/settings', hash: '#updates' }"
        :aria-label="$t('update.badgeAria')"
        class="ml-auto flex items-center gap-1.5 rounded-full border border-accent/50 bg-accent/10 px-2 py-0.5 text-2xs text-accent transition hover:bg-accent/20 motion-reduce:transition-none"
        data-testid="update-badge"
      >
        <span class="size-1.5 rounded-full bg-accent" aria-hidden="true" />
        {{ updates.info.value?.channel === 'beta' ? $t('update.badgeBeta') : $t('update.badge') }}
      </RouterLink>
    </div>

    <nav :aria-label="$t('nav.main')" class="flex shrink-0 flex-col gap-0.5 px-2 pb-3">
      <SidebarLink to="/" icon="message" :label="$t('nav.conversations')" :active="onConversations" :count="store.attention.value" urgent />
      <SidebarLink to="/alerts" icon="bell" :label="$t('nav.alerts')" :active="route.name === 'alerts'" :count="store.alerts.value.length" />
      <SidebarLink v-if="memoryOn" to="/memory" icon="book" :label="$t('nav.memory')" :active="route.name === 'memory'" />
      <SidebarLink
        to="/connectors"
        icon="plug"
        :label="$t('nav.connectors')"
        :active="route.name === 'connectors'"
        :dot-label="hint.visible.value ? $t('extras.hint.text', { names: hint.names.value }) : undefined"
      />
      <SidebarLink to="/settings" icon="settings" :label="$t('nav.settings')" :active="route.name === 'settings'" />
    </nav>

    <ConversationList
      class="border-t border-line-soft"
      :requests="store.requests.value"
      :topics="store.topics.value"
      :selected-id="selectedId"
      :attention="store.attention.value"
      :loaded="store.loaded.value"
      @select="select"
    />
    <SidebarUsage v-if="usage" :usage="usage" />
  </aside>
</template>
