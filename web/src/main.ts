import { createApp } from 'vue';
import App from './App.vue';
import { createApi } from './api';
import { createConnectorsStore } from './composables/useConnectors';
import { provideConnectors } from './composables/useConnectorsStore';
import { provideApi } from './composables/useApi';
import { createAppRouter } from './router';
import './style.css';

const api = await createApi();
const connectors = createConnectorsStore(api);
const app = createApp(App);
provideApi(app, api);
provideConnectors(app, connectors);
app.use(createAppRouter(connectors));
app.mount('#app');
