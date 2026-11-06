import { createApp } from 'vue';
import App from './App.vue';
import { createApi } from './api';
import { createConnectorsStore } from './composables/useConnectors';
import { provideConnectors } from './composables/useConnectorsStore';
import { provideApi } from './composables/useApi';
import { i18n, languageEpoch } from './i18n';
import { createAppRouter } from './router';
import './style.css';

const api = await createApi();
const connectors = createConnectorsStore(api);
window.addEventListener('pouple:language-changed', () => {
  void connectors.refresh().then(() => { languageEpoch.value += 1; });
});
// The core owns the language: read it before the first screen, so nobody sees the wrong one flash by.
await connectors.refreshState().catch(() => undefined);
const app = createApp(App);
app.use(i18n);
provideApi(app, api);
provideConnectors(app, connectors);
app.use(createAppRouter(connectors));
app.mount('#app');
