<script setup lang="ts">
import { ref } from 'vue';
import type { RemoteDevice } from '../api/types';
import { deviceStatus } from '../lib/phonePairing';
import { formatDay, relativeTime } from '../lib/format';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';

defineProps<{
  devices: RemoteDevice[] | null;
  loading: boolean;
  error: string | null;
  revokeError: string | null;
  revoking: ReadonlySet<string>;
}>();
const emit = defineEmits<{ revoke: [id: string]; retry: [] }>();

/** The device whose revocation waits for a second click: revoking cannot be undone, the phone must pair again. */
const confirming = ref<string | null>(null);

function confirm(id: string): void {
  confirming.value = null;
  emit('revoke', id);
}
</script>

<template>
  <div class="flex flex-col gap-2" data-testid="phone-devices">
    <p v-if="loading && devices === null" role="status" class="flex items-center gap-2 py-4 text-sm text-ink-muted" data-testid="devices-loading"><AppSpinner />{{ $t('phone.devices.loading') }}</p>

    <div v-else-if="error && devices === null" class="flex flex-col items-start gap-2" data-testid="devices-error">
      <InlineError box>{{ $t('phone.devices.loadError', { error }) }}</InlineError>
      <AppButton variant="outline" size="sm" @click="emit('retry')">{{ $t('phone.retry') }}</AppButton>
    </div>

    <p v-else-if="devices !== null && devices.length === 0" class="py-4 text-sm text-ink-muted" data-testid="devices-empty">{{ $t('phone.devices.empty') }}</p>

    <template v-else-if="devices !== null">
      <InlineError v-if="error">{{ $t('phone.devices.refreshError', { error }) }}</InlineError>
      <InlineError v-if="revokeError" data-testid="revoke-error">{{ $t('phone.devices.revokeError', { error: revokeError }) }}</InlineError>
      <ul class="flex flex-col divide-y divide-line">
        <li v-for="device in devices" :key="device.id" class="flex flex-col gap-2 py-3" :data-testid="`device-${device.id}`">
          <div class="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
            <div class="min-w-0">
              <p class="flex flex-wrap items-center gap-2 text-sm font-medium text-ink">
                <span class="min-w-0 break-words" :class="{ 'text-ink-muted line-through': deviceStatus(device) === 'revoked' }">{{ device.name }}</span>
                <span
                  class="rounded-full border px-2 py-0.5 text-2xs font-normal"
                  :class="{
                    'border-ok/50 text-ok': deviceStatus(device) === 'active',
                    'border-line-strong text-ink-muted': deviceStatus(device) === 'revoked',
                    'border-warn/50 text-warn': deviceStatus(device) === 'expired',
                  }"
                  :data-testid="`device-status-${device.id}`"
                >{{ $t(`phone.devices.status.${deviceStatus(device)}`) }}</span>
              </p>
              <p class="text-xs text-ink-muted">
                {{ $t('phone.devices.pairedOn', { date: formatDay(device.createdAt) }) }} · {{ $t('phone.devices.lastSeen', { when: relativeTime(device.lastSeen) }) }}
              </p>
            </div>
            <AppButton
              v-if="!device.revoked && confirming !== device.id"
              variant="danger"
              size="sm"
              :disabled="revoking.has(device.id)"
              :data-testid="`revoke-${device.id}`"
              @click="confirming = device.id"
            >
              <AppSpinner v-if="revoking.has(device.id)" />
              {{ $t('phone.devices.revoke') }}
            </AppButton>
          </div>
          <div v-if="confirming === device.id" role="alertdialog" :aria-label="$t('phone.devices.confirmTitle', { name: device.name })" class="flex flex-col gap-2 rounded-lg border border-fail/40 bg-fail/10 p-3" :data-testid="`revoke-confirm-${device.id}`">
            <p class="text-sm text-ink">{{ $t('phone.devices.confirmTitle', { name: device.name }) }}</p>
            <p class="text-xs text-ink-muted">{{ $t('phone.devices.confirmHelp') }}</p>
            <div class="flex flex-wrap gap-2">
              <AppButton variant="outline" size="sm" :data-testid="`revoke-cancel-${device.id}`" @click="confirming = null">{{ $t('phone.devices.confirmCancel') }}</AppButton>
              <AppButton variant="danger" size="sm" :data-testid="`revoke-yes-${device.id}`" @click="confirm(device.id)">{{ $t('phone.devices.confirmYes') }}</AppButton>
            </div>
          </div>
        </li>
      </ul>
    </template>
  </div>
</template>
