<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';

defineProps<{ payload: string }>();

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
    <!-- Seam: a QR renderer takes this slot later and draws `payload`; the text below stays as the fallback. -->
    <slot name="visual" :payload="payload" />
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
