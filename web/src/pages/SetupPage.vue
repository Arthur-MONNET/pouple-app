<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useConnectorsStore } from '../composables/useConnectorsStore';
import { afterSave, requiredSteps, resumePosition, type WizardPosition } from '../lib/connectorFlow';
import AppButton from '../components/AppButton.vue';
import InlineError from '../components/InlineError.vue';
import BrandMark from '../components/BrandMark.vue';
import ConnectorStep from '../components/ConnectorStep.vue';
import LanguageSwitch from '../components/LanguageSwitch.vue';
import { afterExtras, afterProjects, hasExtrasStep } from '../lib/optionalConnectors';
import WizardDone from '../components/WizardDone.vue';
import WizardStepper from '../components/WizardStepper.vue';
import WizardExtras from '../components/WizardExtras.vue';
import WizardProjects from '../components/WizardProjects.vue';
import WizardWelcome from '../components/WizardWelcome.vue';

const router = useRouter();
const store = useConnectorsStore();

const position = ref<WizardPosition | null>(null);
const steps = computed(() => requiredSteps(store.connectors.value));
const withExtras = computed(() => hasExtrasStep(store.connectors.value));
const current = computed(() => {
  const required = steps.value.length;
  switch (position.value?.kind) {
    case 'step':
      return position.value.index;
    case 'projects':
      return required;
    case 'extras':
      return required + 1;
    case 'done':
      return required + 1 + (withExtras.value ? 1 : 0);
    default:
      return 0;
  }
});
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
      <div class="flex flex-wrap items-center gap-4">
        <WizardStepper v-if="position" :items="steps.map((s) => ({ id: s.descriptor.id, name: s.descriptor.name, connected: s.connected }))" :current="current" :extras="withExtras" />
        <LanguageSwitch />
      </div>
    </header>

    <main class="mx-auto w-full max-w-5xl flex-1 px-5 pb-10 min-[700px]:px-8">
      <InlineError v-if="store.error.value" box>
        {{ $t('setup.coreDown', { error: store.error.value }) }}
        <AppButton variant="ghost" size="sm" class="ml-2" @click="store.refresh()">{{ $t('common.tryAgain') }}</AppButton>
      </InlineError>
      <p v-if="position === null && !store.error.value" class="py-20 text-center text-ink-dim" role="status">{{ $t('common.loading') }}</p>
      <ConnectorStep v-else-if="currentConnector" :key="currentConnector.descriptor.id" :connector="currentConnector" :step-number="current + 1" @saved="advance" />
      <WizardProjects v-else-if="position?.kind === 'projects'" @next="position = afterProjects(store.connectors.value)" />
      <WizardExtras v-else-if="position?.kind === 'extras'" @next="position = afterExtras()" />
      <WizardDone v-else-if="position?.kind === 'done'" @open="openMain" />
    </main>
  </div>
</template>
