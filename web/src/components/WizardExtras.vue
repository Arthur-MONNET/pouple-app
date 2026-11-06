<script setup lang="ts">
import { computed, reactive } from 'vue';
import { useConnectorsStore } from '../composables/useConnectorsStore';
import { extraCards, extrasReducer, initialExtras, optionalConnectors, type ExtrasEvent } from '../lib/optionalConnectors';
import AppButton from './AppButton.vue';
import ConnectorStep from './ConnectorStep.vue';
import ExtraConnectorCard from './ExtraConnectorCard.vue';
import SideSheet from './SideSheet.vue';

/** Leaving the step, by continuing or by skipping: it never blocks the setup. */
const emit = defineEmits<{ next: [] }>();

const store = useConnectorsStore();
const sheet = reactive({ ...initialExtras });
const dispatch = (event: ExtrasEvent): void => void Object.assign(sheet, extrasReducer({ ...sheet }, event));

const cards = computed(() => extraCards(store.connectors.value));
/** Read from the store, so the sheet follows a language switch. */
const open = computed(() => optionalConnectors(store.connectors.value).find((c) => c.descriptor.id === sheet.openId) ?? null);
const anyConnected = computed(() => cards.value.some((card) => card.connected));

async function saved(): Promise<void> {
  dispatch({ type: 'saved' });
  await store.refresh();
}
</script>

<template>
  <div class="mx-auto flex max-w-4xl flex-col gap-6 py-6 min-[700px]:py-10">
    <header class="flex flex-col gap-1">
      <p class="text-xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('extras.optional') }}</p>
      <h1 class="font-display text-3xl font-semibold text-ink">{{ $t('extras.title') }}</h1>
      <p class="text-sm text-ink-muted">{{ $t('extras.intro') }}</p>
    </header>

    <ul class="grid gap-3 min-[700px]:grid-cols-2 min-[960px]:grid-cols-3" :aria-label="$t('extras.listLabel')">
      <li v-for="card in cards" :key="card.id">
        <ExtraConnectorCard :card="card" @connect="dispatch({ type: 'open', id: card.id })" />
      </li>
    </ul>

    <div class="flex flex-wrap items-center gap-2">
      <AppButton :variant="anyConnected ? 'outline' : 'primary'" size="lg" class="px-5" data-testid="extras-skip" @click="emit('next')">{{ $t('extras.skip') }}</AppButton>
      <AppButton :variant="anyConnected ? 'primary' : 'outline'" size="lg" class="px-5" data-testid="extras-continue" @click="emit('next')">{{ $t('extras.continue') }}</AppButton>
    </div>

    <SideSheet v-if="open" :title="$t('connectors.connect', { name: open.descriptor.name })" @close="dispatch({ type: 'close' })">
      <ConnectorStep :key="open.descriptor.id" :connector="open" compact @saved="saved" />
    </SideSheet>
  </div>
</template>
