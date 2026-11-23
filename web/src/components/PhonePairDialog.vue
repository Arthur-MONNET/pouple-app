<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue';
import type { PhoneAccess } from '../composables/usePhoneAccess';
import { qrMatrix } from '../lib/pairingQr';
import { formatCountdown, windowShare } from '../lib/phonePairing';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import AppSpinner from './AppSpinner.vue';
import IconButton from './IconButton.vue';
import InlineError from './InlineError.vue';

const props = defineProps<{ access: PhoneAccess }>();
const { phase, offer, remaining } = props.access;

const panel = ref<HTMLElement | null>(null);
const copied = ref(false);
const copyFailed = ref(false);
let copiedTimer: ReturnType<typeof setTimeout> | undefined;

const qr = computed(() => (offer.value ? qrMatrix(offer.value.payload) : null));

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Closing a window that is still open cancels it on the Mac; a finished one (expired, paired, error) is just dismissed. */
function close(): void {
  if (phase.value === 'open' || phase.value === 'opening') void props.access.cancelPairing();
  else props.access.dismissPairing();
}

/** Escape closes, Tab stays inside the dialog. */
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.stopPropagation();
    close();
    return;
  }
  if (event.key !== 'Tab' || !panel.value) return;
  const items = [...panel.value.querySelectorAll<HTMLElement>(FOCUSABLE)];
  const first = items[0];
  const last = items.at(-1);
  if (!first || !last) {
    event.preventDefault();
    panel.value.focus();
  } else if (event.shiftKey && document.activeElement === first) (event.preventDefault(), last.focus());
  else if (!event.shiftKey && document.activeElement === last) (event.preventDefault(), first.focus());
}

async function copy(): Promise<void> {
  if (!offer.value) return;
  clearTimeout(copiedTimer);
  try {
    await navigator.clipboard.writeText(offer.value.payload);
    copied.value = true;
    copyFailed.value = false;
    copiedTimer = setTimeout(() => (copied.value = false), 2000);
  } catch {
    // Without clipboard access the code is shown, selectable, below.
    copyFailed.value = true;
    copied.value = false;
  }
}

onMounted(async () => {
  await nextTick();
  panel.value?.focus();
});
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-40 flex items-center justify-center overflow-y-auto p-4" @keydown="onKeydown">
      <div class="absolute inset-0 bg-black/60" aria-hidden="true" @click="close" />
      <div
        ref="panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="pair-dialog-title"
        tabindex="-1"
        data-testid="phone-pairing"
        class="relative my-auto flex w-full max-w-md flex-col gap-5 rounded-2xl border border-line-strong bg-surface p-6 shadow-2xl outline-none"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <h2 id="pair-dialog-title" class="font-display text-2xl font-semibold text-ink">{{ $t('phone.pairing.title') }}</h2>
            <p class="mt-1.5 text-sm leading-relaxed text-ink-muted">{{ $t('phone.pairing.intro') }}</p>
          </div>
          <IconButton :label="$t('common.close')" icon="close" data-testid="pairing-close" @click="close" />
        </div>

        <p v-if="phase === 'opening'" role="status" class="flex items-center gap-2 text-sm text-ink-muted" data-testid="pairing-opening"><AppSpinner />{{ $t('phone.pairing.opening') }}</p>

        <template v-else-if="phase === 'open' && offer && qr">
          <div class="flex flex-col items-center gap-3.5 rounded-2xl bg-lavender p-5">
            <!-- Always dark on white, whatever the theme: phone cameras need the contrast. -->
            <svg :viewBox="`0 0 ${qr.size} ${qr.size}`" class="size-56 rounded-xl bg-white" shape-rendering="crispEdges" role="img" :aria-label="$t('phone.pairing.qrLabel')" data-testid="pairing-qr">
              <path :d="qr.path" fill="#000" />
            </svg>
            <div class="flex w-full flex-col gap-1.5">
              <!-- The timer is read out once per window start, not every second. -->
              <p class="flex justify-between text-xs font-medium text-on-lavender"><span>{{ $t('phone.pairing.remaining') }}</span><span class="font-mono" role="timer" aria-live="off" data-testid="pairing-countdown">{{ formatCountdown(remaining) }}</span></p>
              <div class="h-1 overflow-hidden rounded-full bg-on-lavender/20" aria-hidden="true"><div class="h-full origin-left bg-accent transition-transform duration-1000 ease-linear motion-reduce:transition-none" :style="{ transform: `scaleX(${windowShare(remaining)})` }" /></div>
            </div>
          </div>
          <ol class="flex flex-col gap-2.5 text-sm text-ink">
            <li class="flex items-center gap-2.5"><span class="grid size-5.5 shrink-0 place-items-center rounded-full border border-line-bright text-xs text-ink-muted" aria-hidden="true">1</span>{{ $t('phone.pairing.step1') }}</li>
            <li class="flex items-center gap-2.5"><span class="grid size-5.5 shrink-0 place-items-center rounded-full border border-line-bright text-xs text-ink-muted" aria-hidden="true">2</span><span>{{ $t('phone.pairing.step2Before') }} <b class="font-semibold">{{ $t('phone.pairing.step2Action') }}</b> {{ $t('phone.pairing.step2After') }}</span></li>
          </ol>
          <code v-if="copyFailed" class="max-h-28 select-all overflow-y-auto break-all rounded-lg border border-line-strong bg-canvas px-3 py-2 font-mono text-xs text-ink" data-testid="pairing-payload-text">{{ offer.payload }}</code>
          <p v-if="copyFailed" role="status" class="text-xs text-ink-muted">{{ $t('phone.pairing.copyFailed') }}</p>
          <div class="flex items-center gap-2.5 border-t border-line pt-4">
            <AppButton variant="outline" size="lg" data-testid="pairing-copy" @click="copy">
              <AppIcon :name="copied ? 'check' : 'copy'" size="sm" />
              <span :aria-live="copied ? 'polite' : 'off'">{{ copied ? $t('phone.pairing.copied') : $t('phone.pairing.copyCode') }}</span>
            </AppButton>
            <AppButton variant="ghost" size="lg" class="ml-auto" data-testid="pairing-cancel" @click="close">{{ $t('phone.pairing.cancel') }}</AppButton>
          </div>
        </template>

        <div v-else-if="phase === 'expired'" class="flex flex-col items-start gap-3" data-testid="pairing-expired">
          <p role="status" class="text-sm text-ink">{{ $t('phone.pairing.expired') }}</p>
          <div class="flex flex-wrap gap-2">
            <AppButton variant="primary" size="lg" :disabled="!access.canPair.value" data-testid="pairing-restart" @click="access.startPairing()">{{ $t('phone.pairing.restart') }}</AppButton>
            <AppButton variant="outline" size="lg" @click="close">{{ $t('phone.pairing.close') }}</AppButton>
          </div>
        </div>

        <div v-else-if="phase === 'paired'" class="flex flex-col items-start gap-3" data-testid="pairing-paired">
          <p role="status" class="text-sm text-ok">{{ $t('phone.pairing.paired', { name: access.pairedName.value ?? '' }) }}</p>
          <AppButton variant="outline" size="lg" @click="close">{{ $t('phone.pairing.close') }}</AppButton>
        </div>

        <div v-else-if="phase === 'idle'" class="flex flex-col items-start gap-3">
          <AppButton variant="outline" size="lg" @click="close">{{ $t('phone.pairing.close') }}</AppButton>
        </div>

        <InlineError v-if="access.pairingError.value" box data-testid="pairing-error">{{ $t('phone.pairing.error', { error: access.pairingError.value }) }}</InlineError>
      </div>
    </div>
  </Teleport>
</template>
