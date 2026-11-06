import { createRouter, createWebHistory, type Router } from 'vue-router';
import type { ConnectorsStore } from './composables/useConnectors';

/**
 * Setup decides where the user can go: the main window and the connectors page need a finished setup, and the
 * wizard is only entered when something is missing (or it was already on screen, so its last screen can show).
 */
export function createAppRouter(store: ConnectorsStore): Router {
  let wizardActive = false;

  const router = createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/setup', name: 'setup', component: () => import('./pages/SetupPage.vue') },
      { path: '/', name: 'home', component: () => import('./pages/HomePage.vue'), meta: { shell: true, needsSetup: true } },
      { path: '/c/:id', name: 'conversation', component: () => import('./pages/HomePage.vue'), meta: { shell: true, needsSetup: true } },
      { path: '/connectors', name: 'connectors', component: () => import('./pages/ConnectorsPage.vue'), meta: { shell: true, needsSetup: true } },
      { path: '/settings', name: 'settings', component: () => import('./pages/SettingsPage.vue'), meta: { shell: true, needsSetup: true } },
      { path: '/:rest(.*)*', redirect: '/' },
    ],
  });

  router.beforeEach(async (to) => {
    // Moving between conversations needs no new answer from the core.
    if (to.name === 'conversation' && wizardActive === false && store.state.value?.setupComplete) return true;
    let complete: boolean;
    try {
      complete = (await store.refreshState()).setupComplete;
    } catch {
      // The core is unreachable: let the page open and say so.
      return true;
    }
    if (to.name === 'setup') {
      if (complete && !wizardActive) return { path: '/' };
      wizardActive = true;
      return true;
    }
    if (to.meta['needsSetup'] && !complete) return { path: '/setup' };
    return true;
  });

  return router;
}
