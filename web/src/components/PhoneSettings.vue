<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useApi } from '../composables/useApi';
import { usePhoneAccess } from '../composables/usePhoneAccess';
import { MAX_PORT, MIN_PORT, formatCountdown, interfaceChoices, isValidPort, toggleInterface, windowShare } from '../lib/phonePairing';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';
import PairingPayload from './PairingPayload.vue';
import PhoneExternal from './PhoneExternal.vue';
import PhoneDevices from './PhoneDevices.vue';
import SettingsRow from './SettingsRow.vue';
import SettingsSection from './SettingsSection.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const access = usePhoneAccess(useApi());
const { settings, loading, loadError, saving, saveError, phase, offer, remaining } = access;
void access.load();

const choices = computed(() => (settings.value ? interfaceChoices(settings.value) : []));
const enabled = computed(() => settings.value?.enabled === true);
const portText = ref('');
const portError = ref(false);
watch(() => settings.value?.port, (port) => {
  if (port !== undefined) portText.value = String(port);
}, { immediate: true });

const share = computed(() => windowShare(remaining.value));

function setEnabled(on: boolean): void {
  void access.save({ enabled: on });
}

function setInterface(name: string, on: boolean): void {
  if (!settings.value) return;
  void access.save({ interfaces: toggleInterface(settings.value.interfaces, name, on) });
}

/** The last network of a running access cannot be unticked: the core would refuse to listen on none. */
const isLastChosen = (selected: boolean): boolean => enabled.value && selected && (settings.value?.interfaces.length ?? 0) <= 1;

async function savePort(): Promise<void> {
  const port = Number(portText.value);
  portError.value = !isValidPort(port);
  if (portError.value || port === settings.value?.port) return;
  if (!(await access.save({ port }))) portText.value = String(settings.value?.port ?? '');
}
</script>

<template>
  <div class="flex flex-col gap-6" data-testid="phone-settings">
    <InlineError v-if="loadError && !settings" box>{{ $t('phone.loadError', { error: loadError }) }}</InlineError>
    <p v-if="loading && !settings" role="status" class="flex items-center gap-2 text-sm text-ink-muted" data-testid="phone-loading"><AppSpinner />{{ $t('phone.loading') }}</p>
    <div v-if="loadError && !settings && !loading"><AppButton variant="outline" size="sm" data-testid="phone-retry" @click="access.load()">{{ $t('phone.retry') }}</AppButton></div>

    <template v-if="settings">
      <SettingsSection :title="$t('phone.title')" :description="$t('phone.intro')">
        <div class="py-4">
          <ToggleSwitch :model-value="enabled" :label="$t('phone.enable')" :description="$t('phone.enableHelp')" :disabled="saving" data-testid="phone-enable" @update:model-value="setEnabled" />
        </div>

        <SettingsRow :label="$t('phone.network')" :description="$t('phone.networkHelp')" stacked>
          <p v-if="choices.length === 0" class="text-sm text-ink-muted" data-testid="phone-no-network">{{ $t('phone.noNetwork') }}</p>
          <ul v-else class="flex flex-col gap-2" role="group" :aria-label="$t('phone.network')">
            <li v-for="item in choices" :key="item.name">
              <label class="flex items-start gap-2.5 rounded-lg border border-line px-3 py-2 text-sm text-ink" :class="{ 'opacity-60': saving }">
                <input
                  type="checkbox"
                  class="mt-0.5 size-4 shrink-0 accent-accent"
                  :checked="item.selected"
                  :disabled="saving || isLastChosen(item.selected)"
                  :data-testid="`phone-interface-${item.name}`"
                  @change="setInterface(item.name, ($event.target as HTMLInputElement).checked)"
                />
                <span class="min-w-0">
                  <span class="font-medium">{{ item.name }}</span>
                  <span v-if="item.loopback" class="ml-2 text-xs text-ink-muted">{{ $t('phone.loopback') }}</span>
                  <span v-if="item.missing" class="ml-2 text-xs text-warn">{{ $t('phone.missing') }}</span>
                  <span v-if="item.addresses.length > 0" class="block break-all font-mono text-xs text-ink-muted">{{ item.addresses.join(', ') }}</span>
                </span>
              </label>
            </li>
          </ul>
        </SettingsRow>

        <SettingsRow :label="$t('phone.port')" :description="$t('phone.portHelp', { min: MIN_PORT, max: MAX_PORT })" label-for="phone-port">
          <input
            id="phone-port"
            v-model="portText"
            type="number"
            inputmode="numeric"
            :min="MIN_PORT"
            :max="MAX_PORT"
            step="1"
            :disabled="saving"
            :aria-invalid="portError"
            class="h-10 w-28 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent disabled:opacity-60"
            data-testid="phone-port"
            @change="savePort"
          />
          <InlineError v-if="portError" data-testid="phone-port-error">{{ $t('phone.portInvalid', { min: MIN_PORT, max: MAX_PORT }) }}</InlineError>
        </SettingsRow>

        <template #footer>
          <div class="flex min-h-5 flex-col gap-1 py-3" aria-live="polite">
            <InlineError v-if="saveError" data-testid="phone-save-error">{{ saveError }}</InlineError>
            <p v-if="settings.problem" role="status" class="text-xs text-warn" data-testid="phone-problem">{{ $t('phone.problem', { problem: settings.problem }) }}</p>
            <p v-else-if="enabled && settings.listening.length > 0" class="break-all text-xs text-ink-muted" data-testid="phone-listening">{{ $t('phone.listening', { addresses: settings.listening.join(', ') }) }}</p>
          </div>
        </template>
      </SettingsSection>

      <PhoneExternal :access="access" />

      <SettingsSection :title="$t('phone.pairing.title')" :description="$t('phone.pairing.intro')">
        <div class="flex flex-col gap-3 py-4" data-testid="phone-pairing">
          <p v-if="!access.canPair.value && phase === 'idle'" class="text-sm text-ink-muted" data-testid="pairing-unavailable">{{ $t('phone.pairing.unavailable') }}</p>

          <div v-if="phase === 'idle'" class="flex flex-wrap gap-2">
            <AppButton variant="primary" size="lg" :disabled="!access.canPair.value" data-testid="pairing-start" @click="access.startPairing()">{{ $t('phone.pairing.start') }}</AppButton>
          </div>

          <p v-else-if="phase === 'opening'" role="status" class="flex items-center gap-2 text-sm text-ink-muted" data-testid="pairing-opening"><AppSpinner />{{ $t('phone.pairing.opening') }}</p>

          <template v-else-if="phase === 'open' && offer">
            <div class="flex flex-col gap-1.5">
              <p class="text-sm text-ink">{{ $t('phone.pairing.scan') }}</p>
              <!-- The timer is read out once per window start, not every second. -->
              <p class="text-sm text-ink-muted"><span>{{ $t('phone.pairing.remaining') }}</span> <span class="font-mono font-medium text-ink" role="timer" aria-live="off" data-testid="pairing-countdown">{{ formatCountdown(remaining) }}</span></p>
              <div class="h-1 overflow-hidden rounded-full bg-line" aria-hidden="true"><div class="h-full origin-left bg-accent transition-transform duration-1000 ease-linear motion-reduce:transition-none" :style="{ transform: `scaleX(${share})` }" /></div>
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
      </SettingsSection>

      <SettingsSection v-if="enabled && settings.externalEnabled" :title="$t('phone.address.title')" :description="$t('phone.address.intro')">
        <div class="flex flex-col gap-3 py-4" data-testid="phone-address">
          <p class="text-xs leading-relaxed text-ink-muted" data-testid="address-safe">{{ $t('phone.address.safe') }}</p>
          <p v-if="!access.canShareAddress.value && !access.address.value" class="text-sm text-ink-muted" data-testid="address-unavailable">{{ $t('phone.address.unavailable') }}</p>
          <div v-if="!access.address.value" class="flex flex-wrap gap-2">
            <AppButton variant="outline" size="lg" :disabled="!access.canShareAddress.value || access.addressLoading.value" data-testid="address-show" @click="access.showAddress()">{{ $t('phone.address.show') }}</AppButton>
          </div>
          <p v-if="access.addressLoading.value" role="status" class="flex items-center gap-2 text-sm text-ink-muted" data-testid="address-loading"><AppSpinner />{{ $t('phone.address.loading') }}</p>
          <template v-if="access.address.value">
            <PairingPayload :payload="access.address.value.payload" :label="$t('phone.address.qrLabel')" test-prefix="address" />
            <div><AppButton variant="outline" data-testid="address-hide" @click="access.hideAddress()">{{ $t('phone.address.hide') }}</AppButton></div>
          </template>
          <InlineError v-if="access.addressError.value" box data-testid="address-error">{{ $t('phone.address.error', { error: access.addressError.value }) }}</InlineError>
        </div>
      </SettingsSection>

      <SettingsSection :title="$t('phone.devices.title')" :description="$t('phone.devices.intro')">
        <div class="py-1">
          <PhoneDevices
            :devices="access.devices.value"
            :loading="loading"
            :error="access.devicesError.value"
            :revoke-error="access.revokeError.value"
            :revoking="access.revoking.value"
            @revoke="access.revoke"
            @retry="access.refreshDevices()"
          />
        </div>
      </SettingsSection>
    </template>
  </div>
</template>
