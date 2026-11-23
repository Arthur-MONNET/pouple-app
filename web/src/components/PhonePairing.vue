<script setup lang="ts">
import { nextTick, ref, watch } from 'vue';
import type { PhoneAccess } from '../composables/usePhoneAccess';
import { formatCountdown, windowShare } from '../lib/phonePairing';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';
import PairingPayload from './PairingPayload.vue';

const props = defineProps<{ access: PhoneAccess }>();
const { phase, offer, remaining } = props.access;

const panel = ref<HTMLElement | null>(null);
// The panel replaces the button that opened it: move the focus into it so a keyboard user is not left behind.
watch(phase, async (next, previous) => {
  if (previous === 'idle' && next !== 'idle') {
    await nextTick();
    panel.value?.focus();
  }
});
</script>

<template>
  <div
    v-if="phase !== 'idle' || access.pairingError.value"
    ref="panel"
    tabindex="-1"
    role="region"
    :aria-label="$t('phone.pairing.title')"
    class="flex flex-col gap-3 rounded-lg border border-line-strong bg-canvas p-4 outline-none"
    data-testid="phone-pairing"
  >
    <p v-if="phase === 'opening'" role="status" class="flex items-center gap-2 text-sm text-ink-muted" data-testid="pairing-opening"><AppSpinner />{{ $t('phone.pairing.opening') }}</p>

    <template v-else-if="phase === 'open' && offer">
      <div class="flex flex-col gap-1.5">
        <p class="text-sm text-ink">{{ $t('phone.pairing.scan') }}</p>
        <!-- The timer is read out once per window start, not every second. -->
        <p class="text-sm text-ink-muted"><span>{{ $t('phone.pairing.remaining') }}</span> <span class="font-mono font-medium text-ink" role="timer" aria-live="off" data-testid="pairing-countdown">{{ formatCountdown(remaining) }}</span></p>
        <div class="h-1 overflow-hidden rounded-full bg-line" aria-hidden="true"><div class="h-full origin-left bg-accent transition-transform duration-1000 ease-linear motion-reduce:transition-none" :style="{ transform: `scaleX(${windowShare(remaining)})` }" /></div>
      </div>
      <PairingPayload :payload="offer.payload" />
      <p class="text-xs text-ink-muted">{{ $t('phone.pairing.once') }}</p>
      <div><AppButton variant="outline" data-testid="pairing-cancel" @click="access.cancelPairing()">{{ $t('phone.pairing.cancel') }}</AppButton></div>
    </template>

    <div v-else-if="phase === 'expired'" class="flex flex-col items-start gap-2" data-testid="pairing-expired">
      <p role="status" class="text-sm text-ink">{{ $t('phone.pairing.expired') }}</p>
      <div class="flex flex-wrap gap-2">
        <AppButton variant="primary" size="lg" :disabled="!access.canPair.value" data-testid="pairing-restart" @click="access.startPairing()">{{ $t('phone.pairing.restart') }}</AppButton>
        <AppButton variant="outline" size="lg" @click="access.dismissPairing()">{{ $t('phone.pairing.close') }}</AppButton>
      </div>
    </div>

    <div v-else-if="phase === 'paired'" class="flex flex-col items-start gap-2" data-testid="pairing-paired">
      <p role="status" class="text-sm text-ok">{{ $t('phone.pairing.paired', { name: access.pairedName.value ?? '' }) }}</p>
      <AppButton variant="outline" size="lg" @click="access.dismissPairing()">{{ $t('phone.pairing.close') }}</AppButton>
    </div>

    <InlineError v-if="access.pairingError.value" box data-testid="pairing-error">{{ $t('phone.pairing.error', { error: access.pairingError.value }) }}</InlineError>
  </div>
</template>
