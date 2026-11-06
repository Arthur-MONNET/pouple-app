<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useConnectorsStore } from '../composables/useConnectorsStore';
import { afterSave, requiredSteps, resumePosition, type WizardPosition } from '../lib/connectorFlow';
import AppButton from '../components/AppButton.vue';
import BrandMark from '../components/BrandMark.vue';
import ConnectorStep from '../components/ConnectorStep.vue';
import WizardDone from '../components/WizardDone.vue';
import WizardStepper from '../components/WizardStepper.vue';
import WizardProjects from '../components/WizardProjects.vue';
import WizardWelcome from '../components/WizardWelcome.vue';

const router = useRouter();
const store = useConnectorsStore();

const position = ref<WizardPosition | null>(null);
const steps = computed(() => requiredSteps(store.connectors.value));
const current = computed(() => (position.value?.kind === 'step' ? position.value.index : position.value?.kind === 'projects' ? steps.value.length : position.value ? steps.value.length + 1 : 0));
const currentConnector = computed(() => (position.value?.kind === 'step' ? (steps.value[position.value.index] ?? null) : null));

onMounted(async () => {
  await store.refresh();
  position.value = resumePosition(steps.value);
});

function start(): void {
  position.value = { kind: 'step', index: 0 };
}

/** The step was saved: the core knows it is connected, so the next position is read from it. */
async function advance(): Promise<void> {
  await store.refresh();
  position.value = afterSave(resumePosition(steps.value));
}

const openMain = (): void => void router.push('/');
</script>

<template>
  <WizardWelcome v-if="position?.kind === 'welcome'" @start="start" />
  <div v-else class="flex min-h-full flex-col">
    <header class="flex flex-wrap items-center justify-between gap-4 px-5 py-4 min-[700px]:px-8">
      <BrandMark />
      <WizardStepper v-if="position" :items="steps.map((s) => ({ id: s.descriptor.id, name: s.descriptor.name, connected: s.connected }))" :current="current" />
    </header>

    <main class="mx-auto w-full max-w-5xl flex-1 px-5 pb-10 min-[700px]:px-8">
      <p v-if="store.error.value" role="alert" class="mb-4 rounded-lg border border-fail/40 bg-fail/10 px-3 py-2 text-sm text-fail">
        The core is not answering: {{ store.error.value }}
        <AppButton variant="ghost" size="sm" class="ml-2" @click="store.refresh()">Try again</AppButton>
      </p>
      <p v-if="position === null && !store.error.value" class="py-20 text-center text-ink-dim" role="status">Loading…</p>
      <ConnectorStep v-else-if="currentConnector" :key="currentConnector.descriptor.id" :connector="currentConnector" :step-number="current + 1" @saved="advance" />
      <WizardProjects v-else-if="position?.kind === 'projects'" @next="position = { kind: 'done' }" />
      <WizardDone v-else-if="position?.kind === 'done'" @open="openMain" />
    </main>
  </div>
</template>
