<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import { useApi } from '../composables/useApi';
import { usePhoneAccess } from '../composables/usePhoneAccess';
import { reachability } from '../lib/phonePairing';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';
import PhoneAdvanced from './PhoneAdvanced.vue';
import PhoneDevices from './PhoneDevices.vue';
import PhonePairDialog from './PhonePairDialog.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const access = usePhoneAccess(useApi());
const { settings, loading, loadError, saving, saveError, phase } = access;
void access.load();

const enabled = computed(() => settings.value?.enabled === true);
const reach = computed(() => (settings.value ? reachability(settings.value) : null));
const dialogOpen = computed(() => phase.value !== 'idle' || access.pairingError.value !== null);

// The dialog is modal: when it closes the focus goes back to the button that opened it.
const addButton = ref<InstanceType<typeof AppButton> | null>(null);
watch(dialogOpen, async (open, wasOpen) => {
  if (wasOpen && !open) {
    await nextTick();
    (addButton.value?.$el as HTMLElement | undefined)?.focus();
  }
});
</script>

<template>
  <div class="flex flex-col gap-7" data-testid="phone-settings">
    <InlineError v-if="loadError && !settings" box>{{ $t('phone.loadError', { error: loadError }) }}</InlineError>
    <p v-if="loading && !settings" role="status" class="flex items-center gap-2 text-sm text-ink-muted" data-testid="phone-loading"><AppSpinner />{{ $t('phone.loading') }}</p>
    <div v-if="loadError && !settings && !loading"><AppButton variant="outline" size="sm" data-testid="phone-retry" @click="access.load()">{{ $t('phone.retry') }}</AppButton></div>

    <template v-if="settings && reach">
      <section class="flex flex-col gap-4 rounded-2xl border border-line-strong bg-surface p-5" :aria-label="$t('phone.enable')" data-testid="phone-card">
        <ToggleSwitch :model-value="enabled" :label="$t('phone.enable')" :description="$t('phone.enableHelp')" :disabled="saving" data-testid="phone-enable" @update:model-value="access.setAccess" />

        <div class="flex flex-col gap-1 empty:hidden">
          <InlineError v-if="saveError" data-testid="phone-save-error">{{ saveError }}</InlineError>
          <p v-if="settings.problem" role="status" class="text-xs text-warn" data-testid="phone-problem">{{ $t('phone.problem', { problem: settings.problem }) }}</p>
        </div>

        <ul v-if="enabled" class="flex flex-col gap-2.5 border-t border-line pt-3.5" data-testid="phone-reach">
          <li v-for="line in [{ id: 'home', title: $t('phone.reach.home'), reach: reach.home }, { id: 'away', title: $t('phone.reach.away'), reach: reach.away }]" :key="line.id" class="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm" :data-testid="`reach-${line.id}`">
            <span
              class="size-2 shrink-0 rounded-full"
              :class="{ 'bg-ok': line.reach.tone === 'ok', 'bg-warn': line.reach.tone === 'warn', 'bg-fail': line.reach.tone === 'fail', 'bg-ink-faint': line.reach.tone === 'idle' }"
              aria-hidden="true"
            />
            <span class="font-medium text-ink">{{ line.title }}</span>
            <span class="text-ink-muted">{{ $t(`phone.reach.${line.reach.key}`) }}</span>
            <span v-if="line.reach.value" class="break-all font-mono text-xs text-ink-dim">{{ line.reach.value }}</span>
          </li>
        </ul>
      </section>

      <section v-if="enabled" class="flex flex-col" aria-labelledby="phone-devices-title" data-testid="phone-devices-area">
        <div class="flex items-center justify-between gap-4">
          <h2 id="phone-devices-title" class="text-base font-semibold text-ink">{{ $t('phone.devices.title') }}</h2>
          <AppButton ref="addButton" variant="primary" size="lg" :disabled="!access.canPair.value" data-testid="pairing-start" @click="access.startPairing()">{{ $t('phone.pairing.start') }}</AppButton>
        </div>
        <div class="mt-3 border-t border-line">
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

      <PhonePairDialog v-if="dialogOpen" :access="access" />
    </template>
  </div>
</template>
