<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { errorMessage } from '../api/client';
import type { ConnectorStatus } from '../api/types';
import AppIcon from '../components/AppIcon.vue';
import ConnectorCard from '../components/ConnectorCard.vue';
import ConnectorStep from '../components/ConnectorStep.vue';
import SideSheet from '../components/SideSheet.vue';
import { useApi } from '../composables/useApi';
import { useConnectorsStore } from '../composables/useConnectorsStore';

const api = useApi();
const store = useConnectorsStore();

const busy = ref(false);
const actionError = ref<string | null>(null);
const reconnecting = ref<ConnectorStatus | null>(null);

const sorted = computed(() => [...store.connectors.value].sort((a, b) => a.descriptor.order - b.descriptor.order));
const connected = computed(() => sorted.value.filter((c) => c.connected || c.descriptor.required));
const available = computed(() => sorted.value.filter((c) => !c.connected && !c.descriptor.required));

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
  reconnecting.value = null;
  await store.refresh();
}
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto">
    <div class="mx-auto flex max-w-4xl flex-col gap-8 px-5 py-6">
      <header>
        <h1 class="text-title font-semibold text-ink">Connectors</h1>
        <p class="text-sm text-ink-muted">The services Pouple reads from and works with. Secrets stay in your keychain.</p>
      </header>

      <p v-if="store.error.value" role="alert" class="rounded-lg border border-fail/40 bg-fail/10 px-3 py-2 text-sm text-fail">The core is not answering: {{ store.error.value }}</p>
      <p v-if="actionError" role="alert" class="rounded-lg border border-fail/40 bg-fail/10 px-3 py-2 text-sm text-fail">{{ actionError }}</p>

      <section aria-labelledby="connected-title" class="flex flex-col gap-3">
        <h2 id="connected-title" class="text-xs font-medium uppercase tracking-wider text-ink-dim">Your connectors</h2>
        <ul class="grid gap-3 min-[700px]:grid-cols-2">
          <li v-for="connector in connected" :key="connector.descriptor.id" class="flex">
            <ConnectorCard class="w-full" :connector="connector" :busy="busy" @reconnect="reconnecting = connector" @disconnect="disconnect(connector)" />
          </li>
        </ul>
      </section>

      <section aria-labelledby="add-title" class="flex flex-col gap-3">
        <h2 id="add-title" class="text-xs font-medium uppercase tracking-wider text-ink-dim">Add a connector</h2>
        <ul v-if="available.length > 0" class="grid gap-3 min-[700px]:grid-cols-2">
          <li v-for="connector in available" :key="connector.descriptor.id" class="flex">
            <ConnectorCard class="w-full" :connector="connector" :busy="busy" @reconnect="reconnecting = connector" @disconnect="disconnect(connector)" />
          </li>
        </ul>
        <div v-else class="flex items-center gap-4 rounded-xl border border-dashed border-line-strong px-5 py-6" data-testid="more-coming">
          <span class="flex size-10 shrink-0 items-center justify-center rounded-xl bg-raised text-ink-muted"><AppIcon name="plug" size="lg" /></span>
          <div>
            <p class="text-sm font-medium text-ink">More connectors are coming</p>
            <p class="text-xs text-ink-muted">Slite, Asana and GitLab are next. They will appear here, with the same guided setup.</p>
          </div>
        </div>
      </section>
    </div>

    <SideSheet v-if="reconnecting" :title="`${reconnecting.connected ? 'Reconnect' : 'Connect'} ${reconnecting.descriptor.name}`" @close="reconnecting = null">
      <ConnectorStep :key="reconnecting.descriptor.id" :connector="reconnecting" compact @saved="saved" />
    </SideSheet>
  </main>
</template>
