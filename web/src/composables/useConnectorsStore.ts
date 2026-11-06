import { inject, type App, type InjectionKey } from 'vue';
import type { ConnectorsStore } from './useConnectors';

const KEY: InjectionKey<ConnectorsStore> = Symbol('pouple-connectors');

export const provideConnectors = (app: App, store: ConnectorsStore): void => void app.provide(KEY, store);

export function useConnectorsStore(): ConnectorsStore {
  const store = inject(KEY);
  if (!store) throw new Error('useConnectorsStore() called outside of an app that provides it');
  return store;
}
