import { computed, ref } from 'vue';
import { errorMessage, type PoupleApi } from '../api/client';
import type { AppState, ConnectorStatus } from '../api/types';

/** What the app knows about setup and connectors; shared by the router guard, the wizard and the connectors page. */
export function createConnectorsStore(api: PoupleApi) {
  const state = ref<AppState | null>(null);
  const connectors = ref<ConnectorStatus[]>([]);
  const error = ref<string | null>(null);
  const loading = ref(false);

  async function refreshState(): Promise<AppState> {
    const next = await api.getState();
    state.value = next;
    return next;
  }

  async function refreshConnectors(): Promise<void> {
    loading.value = true;
    try {
      connectors.value = await api.listConnectors();
      error.value = null;
    } catch (cause) {
      error.value = errorMessage(cause);
    } finally {
      loading.value = false;
    }
  }

  /** Reloads both after something was saved or removed. */
  async function refresh(): Promise<void> {
    await Promise.all([refreshState().catch(() => undefined), refreshConnectors()]);
  }

  return {
    state: computed(() => state.value),
    connectors: computed(() => connectors.value),
    error: computed(() => error.value),
    loading: computed(() => loading.value),
    refreshState, refreshConnectors, refresh };
}

export type ConnectorsStore = ReturnType<typeof createConnectorsStore>;
