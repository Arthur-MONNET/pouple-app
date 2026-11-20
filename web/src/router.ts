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
      {
        // Every screen after setup shares one shell: the sidebar with the conversations stays in place between them.
        path: '/',
        component: () => import('./components/AppShell.vue'),
        meta: { needsSetup: true },
        children: [
          { path: '', name: 'home', component: () => import('./pages/ConversationsPage.vue') },
          { path: 'c/:id', name: 'conversation', component: () => import('./pages/ConversationsPage.vue') },
          { path: 'alerts', name: 'alerts', component: () => import('./pages/AlertsPage.vue') },
          { path: 'memory', name: 'memory', component: () => import('./pages/MemoryPage.vue') },
          { path: 'connectors', name: 'connectors', component: () => import('./pages/ConnectorsPage.vue') },
          { path: 'settings', name: 'settings', component: () => import('./pages/SettingsPage.vue') },
        ],
      },
      { path: '/:rest(.*)*', redirect: '/' },
    ],
  });

  router.beforeEach(async (to) => {
    // Once setup is known to be complete, moving between screens needs no new answer from the core: the screens refresh
    // what they show themselves, and a disconnection is seen by the connectors store, which updates this state.
    if (to.name !== 'setup' && wizardActive === false && store.state.value?.setupComplete) return true;
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
