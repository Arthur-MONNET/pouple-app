import { createApp } from 'vue';
import App from './App.vue';
import { createApi } from './api';
import { createConnectorsStore } from './composables/useConnectors';
import { provideConnectors } from './composables/useConnectorsStore';
import { provideApi } from './composables/useApi';
import { createUpdateStore, provideUpdate } from './composables/useUpdate';
import { i18n, languageEpoch } from './i18n';
import { conversationIdOf, OPEN_CONVERSATION_EVENT } from './lib/openConversation';
import { createAppRouter } from './router';
import './style.css';

// Registered before anything is awaited: the shell may name a conversation the moment the page has loaded, while the
// app is still starting. The id waits here until the router exists.
let waitingConversation: string | null = null;
let showConversation: (id: string) => void = (id) => { waitingConversation = id; };
window.addEventListener(OPEN_CONVERSATION_EVENT, (event) => {
  const id = conversationIdOf(event);
  if (id) showConversation(id);
});

const api = await createApi();
const connectors = createConnectorsStore(api);
window.addEventListener('pouple:language-changed', () => {
  void connectors.refresh().then(() => { languageEpoch.value += 1; });
});
// The core owns the language: read it before the first screen, so nobody sees the wrong one flash by.
await connectors.refreshState().catch(() => undefined);
const updates = createUpdateStore(api);
const app = createApp(App);
app.use(i18n);
provideApi(app, api);
provideConnectors(app, connectors);
provideUpdate(app, updates);
const router = createAppRouter(connectors);
app.use(router);
// The bubble's "Open Pouple" button: the shell names the conversation, the router shows it.
showConversation = (id) => void router.push({ name: 'conversation', params: { id } });
if (waitingConversation) showConversation(waitingConversation);
app.mount('#app');
// Once at startup, then every 30 minutes while the window is open; never awaited, errors ignored.
updates.startPolling();
