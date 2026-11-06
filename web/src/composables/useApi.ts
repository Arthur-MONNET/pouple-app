import { inject, type App, type InjectionKey } from 'vue';
import type { PoupleApi } from '../api/client';

const API_KEY: InjectionKey<PoupleApi> = Symbol('pouple-api');

export const provideApi = (app: App, api: PoupleApi): void => void app.provide(API_KEY, api);

export function useApi(): PoupleApi {
  const api = inject(API_KEY);
  if (!api) throw new Error('useApi() called outside of an app that provides the API');
  return api;
}
