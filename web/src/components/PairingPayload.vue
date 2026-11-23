<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue';
import { qrMatrix } from '../lib/pairingQr';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';

const props = defineProps<{ payload: string }>();

const qr = computed(() => qrMatrix(props.payload));

const copied = ref(false);
const failed = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;

async function copy(payload: string): Promise<void> {
  clearTimeout(timer);
  try {
    await navigator.clipboard.writeText(payload);
    copied.value = true;
    failed.value = false;
    timer = setTimeout(() => (copied.value = false), 2000);
  } catch {
    // Without clipboard access the payload stays selectable on screen.
    failed.value = true;
    copied.value = false;
  }
}

onBeforeUnmount(() => clearTimeout(timer));
</script>

<template>
  <div class="flex flex-col gap-2" data-testid="pairing-payload">
    <!-- Always dark on white, whatever the theme: phone cameras need the contrast. -->
    <svg
      :viewBox="`0 0 ${qr.size} ${qr.size}`"
      class="size-56 self-center rounded-lg bg-white"
      shape-rendering="crispEdges"
      role="img"
      :aria-label="$t('phone.pairing.qrLabel')"
      data-testid="pairing-qr"
    >
      <path :d="qr.path" fill="#000" />
    </svg>
    <div class="flex items-start gap-2">
      <code class="max-h-28 min-w-0 flex-1 select-all overflow-y-auto break-all rounded-lg border border-line-strong bg-canvas px-3 py-2 font-mono text-xs text-ink" data-testid="pairing-payload-text">{{ payload }}</code>
      <AppButton variant="outline" size="md" data-testid="pairing-copy" @click="copy(payload)">
        <AppIcon :name="copied ? 'check' : 'copy'" size="sm" />
        <span :aria-live="copied ? 'polite' : 'off'">{{ copied ? $t('phone.pairing.copied') : $t('phone.pairing.copy') }}</span>
      </AppButton>
    </div>
    <p v-if="failed" role="status" class="text-xs text-ink-muted">{{ $t('phone.pairing.copyFailed') }}</p>
  </div>
</template>
