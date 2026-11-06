<script setup lang="ts">
import { computed, toRef } from 'vue';
import { t } from '../i18n';
import type { ConnectorStatus } from '../api/types';
import { useConnectorStep } from '../composables/useConnectorStep';
import ActionsPanel from './ActionsPanel.vue';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import FieldInput from './FieldInput.vue';
import GuideList from './GuideList.vue';
import InlineError from './InlineError.vue';
import MascotStage from './MascotStage.vue';
import ProfileCard from './ProfileCard.vue';

const props = withDefaults(defineProps<{ connector: ConnectorStatus; compact?: boolean; stepNumber?: number }>(), { compact: false, stepNumber: 1 });
/** The connection was confirmed and saved. */
const emit = defineEmits<{ saved: [] }>();

const step = useConnectorStep(toRef(props, 'connector'), () => emit('saved'));
const { flow } = step;
const confirming = computed(() => flow.value.phase === 'confirm' || flow.value.phase === 'saving');

const descriptor = computed(() => props.connector.descriptor);
/** The words of the status line, announced politely to screen readers. */
const statusText = computed(() => {
  switch (flow.value.phase) {
    case 'checking':
      return t('connect.checking', { name: descriptor.value.name });
    case 'confirm':
      return t('connect.found', { profile: flow.value.profile.name });
    case 'saving':
      return t('connect.saving', { name: descriptor.value.name });
    default:
      return '';
  }
});
</script>

<template>
  <div
    class="grid gap-8"
    :class="compact ? 'grid-cols-1' : 'grid-cols-1 min-[700px]:grid-cols-[minmax(0,5fr)_minmax(0,6fr)] min-[700px]:gap-10'"
  >
    <section v-if="!confirming" aria-labelledby="guide-title" class="flex flex-col gap-4">
      <h2 id="guide-title" class="text-xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('guide.howTo', { name: descriptor.name }) }}</h2>
      <GuideList :steps="descriptor.guide ?? []" />
    </section>
    <section v-else class="flex flex-col gap-4" aria-labelledby="confirm-title">
      <p class="font-mono text-xs uppercase tracking-widest text-accent">{{ $t('connect.stepEyebrow', { n: stepNumber, name: descriptor.name }) }}</p>
      <h2 id="confirm-title" class="font-display text-4xl font-semibold leading-tight tracking-tight text-ink">{{ $t('connect.isItYou') }}</h2>
      <p class="text-base leading-relaxed text-ink-muted">{{ $t('connect.answered', { name: descriptor.name }) }}</p>
      <MascotStage v-if="!compact" pose="done" size="lg" class="mt-2" />
    </section>

    <section :aria-labelledby="confirming ? 'confirm-title' : 'form-title'" class="flex min-w-0 flex-col gap-5 rounded-2xl border border-line bg-surface/60 p-5 min-[700px]:p-6">
      <header v-if="!confirming">
        <h2 id="form-title" class="text-lg font-semibold text-ink">{{ descriptor.name }}</h2>
        <p class="text-sm text-ink-muted">{{ descriptor.tagline }}</p>
      </header>

      <p class="sr-only" role="status" aria-live="polite">{{ statusText }}</p>

      <template v-if="flow.phase === 'form'">
        <form v-if="step.withForm.value" class="flex flex-col gap-5" novalidate @submit.prevent="step.check()">
          <FieldInput
            v-for="field in step.fields.value"
            :key="field.id"
            :model-value="step.values[field.id] ?? ''"
            @update:model-value="step.values[field.id] = $event"
            :field="field"
            :error="step.fieldErrors.value[field.id] ?? null"
            @blur="step.touched[field.id] = true"
            @enter="step.check()"
          />
          <InlineError v-if="step.generalError.value">{{ step.generalError.value }}</InlineError>
          <AppButton type="submit" variant="primary" size="lg" class="w-fit" data-testid="check">
            {{ step.fields.value.some((field) => field.secret) ? $t('connect.checkToken') : $t('connect.check') }}
          </AppButton>
        </form>
        <ActionsPanel
          v-else
          :connector-id="descriptor.id"
          :actions="descriptor.actions ?? []"
          :checklist="step.checklist.value"
          :message="step.generalError.value"
          @finished="step.check()"
        />
      </template>

      <div v-else-if="flow.phase === 'checking'" class="flex flex-col gap-4" data-testid="checking" aria-busy="true">
        <p class="flex items-center gap-2.5 text-sm text-ink-muted">
          <AppSpinner />
          {{ $t('connect.checking', { name: descriptor.name }) }}
        </p>
        <div class="flex animate-pulse flex-col gap-3 motion-reduce:animate-none" aria-hidden="true">
          <div class="flex items-center gap-4">
            <div class="size-16 rounded-xl bg-selected" />
            <div class="flex flex-1 flex-col gap-2">
              <div class="h-4 w-2/3 rounded bg-selected" />
              <div class="h-3 w-1/3 rounded bg-raised" />
            </div>
          </div>
          <div class="h-24 rounded-lg bg-raised" />
        </div>
      </div>

      <div v-else class="flex flex-col gap-5" data-testid="confirmation">
        <ProfileCard :profile="flow.profile" />
        <div class="flex flex-wrap gap-2">
          <AppButton variant="primary" size="lg" :disabled="flow.phase === 'saving'" data-testid="confirm" @click="step.confirm()">
            <AppSpinner v-if="flow.phase === 'saving'" />
            {{ $t('connect.yes') }}
          </AppButton>
          <AppButton variant="outline" size="lg" :disabled="flow.phase === 'saving'" data-testid="reject" @click="step.reject()">
            {{ step.withForm.value ? $t('connect.notMeToken') : $t('connect.notMeAccount') }}
          </AppButton>
        </div>
      </div>
    </section>
  </div>
</template>
