<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { PhoneAccess } from '../composables/usePhoneAccess';
import { EXTERNAL_AUTO, MAX_HOST_LENGTH, checkExternalHost, forwardedPort, localAddress, parseExternalPort } from '../lib/phonePairing';
import InlineError from './InlineError.vue';
import SettingsRow from './SettingsRow.vue';
import SettingsSection from './SettingsSection.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const props = defineProps<{ access: PhoneAccess }>();
const { settings, saving } = props.access;

const enabled = computed(() => settings.value?.externalEnabled === true);
const hostText = ref('');
const portText = ref('');
const hostIssue = ref<'required' | 'invalid' | null>(null);
const portError = ref(false);
/** The "find it automatically" switch; turning it off only shows the field, nothing is saved until a host is typed. */
const autoChoice = ref(false);

watch(() => settings.value?.externalHost, (host) => {
  if (host === undefined) return;
  autoChoice.value = host === EXTERNAL_AUTO;
  if (host !== EXTERNAL_AUTO) hostText.value = host;
}, { immediate: true });
watch(() => settings.value?.externalPort, (port) => {
  if (port !== undefined) portText.value = port > 0 ? String(port) : '';
}, { immediate: true });

const forward = computed(() => (settings.value ? forwardedPort(settings.value) : 0));
const lanAddress = computed(() => localAddress(settings.value?.listening ?? []));
const autoActive = computed(() => autoChoice.value && settings.value?.externalHost === EXTERNAL_AUTO);

async function setEnabled(on: boolean): Promise<void> {
  if (!on) {
    void props.access.save({ externalEnabled: false });
    return;
  }
  const check = autoChoice.value ? ({ ok: true, host: EXTERNAL_AUTO } as const) : checkExternalHost(hostText.value);
  const port = parseExternalPort(portText.value);
  hostIssue.value = check.ok ? null : check.reason;
  portError.value = port === null;
  if (!check.ok || port === null) return;
  await props.access.save({ externalEnabled: true, externalHost: check.host, externalPort: port });
}

async function setAuto(on: boolean): Promise<void> {
  autoChoice.value = on;
  hostIssue.value = null;
  if (!on) return;
  if (!(await props.access.save({ externalHost: EXTERNAL_AUTO }))) autoChoice.value = settings.value?.externalHost === EXTERNAL_AUTO;
}

async function saveHost(): Promise<void> {
  if (autoChoice.value) return;
  const check = checkExternalHost(hostText.value);
  if (!check.ok) {
    // An empty field is fine while the access from outside is off: it just clears the stored host.
    if (check.reason === 'required' && !enabled.value) {
      hostIssue.value = null;
      if (settings.value?.externalHost) await props.access.save({ externalHost: '' });
      return;
    }
    hostIssue.value = check.reason;
    return;
  }
  hostIssue.value = null;
  hostText.value = check.host;
  if (check.host !== settings.value?.externalHost) await props.access.save({ externalHost: check.host });
}

async function savePort(): Promise<void> {
  const port = parseExternalPort(portText.value);
  portError.value = port === null;
  if (port === null || port === settings.value?.externalPort) return;
  if (!(await props.access.save({ externalPort: port }))) portText.value = settings.value?.externalPort ? String(settings.value.externalPort) : '';
}
</script>

<template>
  <SettingsSection v-if="settings" :title="$t('phone.external.title')" :description="$t('phone.external.intro')">
    <div class="py-4">
      <ToggleSwitch :model-value="enabled" :label="$t('phone.external.enable')" :description="$t('phone.external.enableHelp')" :disabled="saving" data-testid="external-enable" @update:model-value="setEnabled" />
    </div>

    <div class="py-4">
      <ToggleSwitch :model-value="autoChoice" :label="$t('phone.external.auto')" :description="$t('phone.external.autoHelp')" :disabled="saving" data-testid="external-auto" @update:model-value="setAuto" />
      <p v-if="autoActive && enabled" class="mt-2 break-all text-xs text-ink-muted" role="status" data-testid="external-detected">
        {{ settings.externalDetected ? $t('phone.external.detected', { address: settings.externalDetected }) : $t('phone.external.notDetected') }}
      </p>
    </div>

    <SettingsRow v-if="!autoChoice" :label="$t('phone.external.host')" :description="$t('phone.external.hostHelp')" label-for="external-host" stacked>
      <input
        id="external-host"
        v-model="hostText"
        type="text"
        inputmode="url"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        :maxlength="MAX_HOST_LENGTH"
        :placeholder="$t('phone.external.hostPlaceholder')"
        :disabled="saving"
        :aria-invalid="hostIssue !== null"
        class="h-10 w-full rounded-lg border border-line-strong bg-canvas px-3 font-mono text-sm text-ink outline-none focus:border-accent disabled:opacity-60"
        data-testid="external-host"
        @change="saveHost"
      />
      <InlineError v-if="hostIssue === 'required'" data-testid="external-host-error">{{ $t('phone.external.hostRequired') }}</InlineError>
      <InlineError v-else-if="hostIssue === 'invalid'" data-testid="external-host-error">{{ $t('phone.external.hostInvalid') }}</InlineError>
    </SettingsRow>

    <SettingsRow :label="$t('phone.external.port')" :description="$t('phone.external.portHelp', { port: settings.port })" label-for="external-port">
      <input
        id="external-port"
        v-model="portText"
        type="number"
        inputmode="numeric"
        min="1"
        max="65535"
        step="1"
        :placeholder="String(settings.port)"
        :disabled="saving"
        :aria-invalid="portError"
        class="h-10 w-28 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent disabled:opacity-60"
        data-testid="external-port"
        @change="savePort"
      />
      <InlineError v-if="portError" data-testid="external-port-error">{{ $t('phone.external.portInvalid') }}</InlineError>
    </SettingsRow>

    <div class="flex flex-col gap-2 py-4" data-testid="external-steps">
      <h3 class="text-sm font-medium text-ink">{{ $t('phone.external.stepsTitle') }}</h3>
      <ol class="flex list-decimal flex-col gap-1.5 pl-5 text-sm leading-relaxed text-ink">
        <li data-testid="external-step-1">{{ lanAddress ? $t('phone.external.step1Known', { address: lanAddress }) : $t('phone.external.step1') }}</li>
        <li data-testid="external-step-2">{{ $t('phone.external.step2', { externalPort: forward, port: settings.port }) }}</li>
        <li data-testid="external-step-3">{{ $t('phone.external.step3') }}</li>
      </ol>
    </div>

    <template #footer>
      <div class="flex min-h-5 flex-col gap-1 py-3" aria-live="polite">
        <p v-if="enabled && !settings.enabled" role="status" class="text-xs text-warn" data-testid="external-needs-access">{{ $t('phone.external.needsAccess') }}</p>
        <p v-else-if="enabled && settings.externalReady" role="status" class="text-xs text-ok" data-testid="external-ready">{{ $t('phone.external.ready') }}</p>
        <p v-else-if="enabled" role="status" class="text-xs text-warn" data-testid="external-not-ready">{{ $t('phone.external.notReady') }}</p>
      </div>
    </template>
  </SettingsSection>
</template>
