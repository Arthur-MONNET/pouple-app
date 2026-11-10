<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { errorMessage } from '../api/client';
import type { ConnectorStatus } from '../api/types';
import InlineError from '../components/InlineError.vue';
import ConnectorCard from '../components/ConnectorCard.vue';
import ConnectorStep from '../components/ConnectorStep.vue';
import SideSheet from '../components/SideSheet.vue';
import { connectorSettingsLocation } from '../lib/connectorSettings';
import { optionalConnectors } from '../lib/optionalConnectors';
import { useApi } from '../composables/useApi';
import { useConnectorsStore } from '../composables/useConnectorsStore';

const api = useApi();
const router = useRouter();
const store = useConnectorsStore();

const busy = ref(false);
const actionError = ref<string | null>(null);
const reconnectingId = ref<string | null>(null);
/** Read from the store, so the sheet follows a language switch. */
const reconnecting = computed(() => store.connectors.value.find((c) => c.descriptor.id === reconnectingId.value) ?? null);

const sorted = computed(() => [...store.connectors.value].sort((a, b) => a.descriptor.order - b.descriptor.order));
const required = computed(() => sorted.value.filter((c) => c.descriptor.required));
const extras = computed(() => optionalConnectors(store.connectors.value));

onMounted(() => void store.refresh());

async function disconnect(connector: ConnectorStatus): Promise<void> {
  busy.value = true;
  actionError.value = null;
  try {
    await api.removeConnector(connector.descriptor.id);
    await store.refresh();
  } catch (cause) {
    actionError.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
}

async function saved(): Promise<void> {
  reconnectingId.value = null;
  await store.refresh();
}
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto">
    <div class="mx-auto flex max-w-4xl flex-col gap-8 px-5 py-6">
      <header>
        <h1 class="text-title font-semibold text-ink">{{ $t('connectors.title') }}</h1>
        <p class="text-sm text-ink-muted">{{ $t('connectors.intro') }}</p>
      </header>

      <InlineError v-if="store.error.value" box>{{ $t('connectors.coreDown', { error: store.error.value }) }}</InlineError>
      <InlineError v-if="actionError" box>{{ actionError }}</InlineError>

      <section aria-labelledby="connected-title" class="flex flex-col gap-3">
        <h2 id="connected-title" class="text-xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('connectors.yours') }}</h2>
        <ul class="grid gap-3 min-[700px]:grid-cols-2">
          <li v-for="connector in required" :key="connector.descriptor.id" class="flex">
            <ConnectorCard class="w-full" :connector="connector" :busy="busy" @reconnect="reconnectingId = connector.descriptor.id" @disconnect="disconnect(connector)" @settings="router.push(connectorSettingsLocation(connector.descriptor.id))" />
          </li>
        </ul>
      </section>

      <section v-if="extras.length > 0" aria-labelledby="extras-title" class="flex flex-col gap-3" data-testid="extras-section">
        <div>
          <h2 id="extras-title" class="text-xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('connectors.extrasTitle') }}</h2>
          <p class="text-xs text-ink-muted">{{ $t('connectors.extrasIntro') }}</p>
        </div>
        <ul class="grid gap-3 min-[700px]:grid-cols-2">
          <li v-for="connector in extras" :key="connector.descriptor.id" class="flex">
            <ConnectorCard class="w-full" :connector="connector" :busy="busy" @reconnect="reconnectingId = connector.descriptor.id" @disconnect="disconnect(connector)" @settings="router.push(connectorSettingsLocation(connector.descriptor.id))" />
          </li>
        </ul>
      </section>
    </div>

    <SideSheet v-if="reconnecting" :title="reconnecting.connected ? $t('connectors.reconnect', { name: reconnecting.descriptor.name }) : $t('connectors.connect', { name: reconnecting.descriptor.name })" @close="reconnectingId = null">
      <ConnectorStep :key="reconnecting.descriptor.id" :connector="reconnecting" compact @saved="saved" />
    </SideSheet>
  </main>
</template>
