<script setup lang="ts">
import { computed } from 'vue';
import { useApi } from '../composables/useApi';
import { usePhoneAccess } from '../composables/usePhoneAccess';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';
import PhoneAdvanced from './PhoneAdvanced.vue';
import PhoneDevices from './PhoneDevices.vue';
import PhonePairing from './PhonePairing.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const access = usePhoneAccess(useApi());
const { settings, loading, loadError, saving, saveError, phase } = access;
void access.load();

const enabled = computed(() => settings.value?.enabled === true);
/** On, but nothing listens: no network is up, or the core could not bind. Pairing waits for it. */
const notListening = computed(() => enabled.value && !settings.value?.problem && (settings.value?.listening.length ?? 0) === 0);
</script>

<template>
  <div class="flex flex-col gap-4" data-testid="phone-settings">
    <InlineError v-if="loadError && !settings" box>{{ $t('phone.loadError', { error: loadError }) }}</InlineError>
    <p v-if="loading && !settings" role="status" class="flex items-center gap-2 text-sm text-ink-muted" data-testid="phone-loading"><AppSpinner />{{ $t('phone.loading') }}</p>
    <div v-if="loadError && !settings && !loading"><AppButton variant="outline" size="sm" data-testid="phone-retry" @click="access.load()">{{ $t('phone.retry') }}</AppButton></div>

    <template v-if="settings">
      <section class="flex flex-col gap-4 rounded-xl border border-line bg-surface p-4" :aria-label="$t('phone.enable')" data-testid="phone-card">
        <ToggleSwitch :model-value="enabled" :label="$t('phone.enable')" :description="$t('phone.enableHelp')" :disabled="saving" data-testid="phone-enable" @update:model-value="access.setAccess" />

        <div class="flex flex-col gap-1 empty:hidden">
          <InlineError v-if="saveError" data-testid="phone-save-error">{{ saveError }}</InlineError>
          <p v-if="settings.problem" role="status" class="text-xs text-warn" data-testid="phone-problem">{{ $t('phone.problem', { problem: settings.problem }) }}</p>
          <p v-else-if="notListening" role="status" class="text-xs text-warn" data-testid="phone-not-listening">{{ $t('phone.notListening') }}</p>
        </div>

        <div v-if="enabled" class="flex flex-col gap-3 border-t border-line pt-4" data-testid="phone-devices-area">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 class="text-sm font-semibold text-ink">{{ $t('phone.devices.title') }}</h2>
            <AppButton v-if="phase === 'idle'" variant="primary" size="lg" :disabled="!access.canPair.value" data-testid="pairing-start" @click="access.startPairing()">{{ $t('phone.pairing.start') }}</AppButton>
          </div>
          <PhonePairing :access="access" />
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
      </section>

      <PhoneAdvanced :access="access" />
    </template>
  </div>
</template>
