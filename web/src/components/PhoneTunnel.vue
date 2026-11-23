<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import type { PhoneAccess } from '../composables/usePhoneAccess';
import { MAX_HOST_LENGTH, checkTunnelDomain, tunnelPatch } from '../lib/phonePairing';
import AppButton from './AppButton.vue';
import InlineError from './InlineError.vue';
import SettingsRow from './SettingsRow.vue';
import SettingsSection from './SettingsSection.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const props = defineProps<{ access: PhoneAccess }>();
const { settings, saving } = props.access;

const enabled = computed(() => settings.value?.tunnelEnabled === true);
const status = computed(() => settings.value?.tunnelStatus ?? 'off');
const tokenStored = computed(() => settings.value?.tunnelAuthtokenSet === true);

const domainText = ref('');
/** Write-only: never filled from the settings, cleared as soon as it is saved. */
const tokenText = ref('');
/** The "Replace" action: a stored token is hidden behind a line until the person asks to change it. */
const replacing = ref(false);
const issue = ref<'required' | 'invalid' | 'token' | null>(null);
const tokenInput = ref<HTMLInputElement | null>(null);

watch(() => settings.value?.tunnelDomain, (domain) => {
  if (domain !== undefined) domainText.value = domain;
}, { immediate: true });

const showTokenField = computed(() => !tokenStored.value || replacing.value);
const domainChanged = computed(() => domainText.value.trim().toLowerCase() !== (settings.value?.tunnelDomain ?? ''));
const dirty = computed(() => domainChanged.value || tokenText.value.trim() !== '');
const domainIssue = computed(() => (issue.value === 'required' || issue.value === 'invalid' ? issue.value : null));

async function submit(enable?: boolean): Promise<void> {
  const result = tunnelPatch({ domain: domainText.value, token: tokenText.value, tokenStored: tokenStored.value, enable });
  issue.value = result.ok ? null : result.reason;
  if (!result.ok) return;
  if (await props.access.save(result.patch)) {
    tokenText.value = '';
    replacing.value = false;
  }
}

function setEnabled(on: boolean): void {
  if (on) void submit(true);
  else void props.access.save({ tunnelEnabled: false });
}

/** Typing a domain that is wrong is told while leaving the field, not on every key. */
function checkDomain(): void {
  const check = checkTunnelDomain(domainText.value);
  issue.value = !check.ok && (check.reason === 'invalid' || domainText.value.trim() !== '') ? check.reason : null;
}

async function replace(): Promise<void> {
  replacing.value = true;
  await nextTick();
  tokenInput.value?.focus();
}

function cancelReplace(): void {
  replacing.value = false;
  tokenText.value = '';
  issue.value = null;
}
</script>

<template>
  <SettingsSection v-if="settings" :title="$t('phone.tunnel.title')" :description="$t('phone.tunnel.intro')">
    <div class="flex flex-col gap-2 py-4" data-testid="tunnel-steps">
      <h3 class="text-sm font-medium text-ink">{{ $t('phone.tunnel.stepsTitle') }}</h3>
      <ol class="flex list-decimal flex-col gap-1.5 pl-5 text-sm leading-relaxed text-ink">
        <li>{{ $t('phone.tunnel.step1') }}</li>
        <li>{{ $t('phone.tunnel.step2') }}</li>
        <li>{{ $t('phone.tunnel.step3') }}</li>
      </ol>
      <p class="text-xs leading-relaxed text-ink-muted" data-testid="tunnel-privacy">{{ $t('phone.tunnel.privacy') }}</p>
    </div>

    <SettingsRow :label="$t('phone.tunnel.domain')" :description="$t('phone.tunnel.domainHelp')" label-for="tunnel-domain" stacked>
      <input
        id="tunnel-domain"
        v-model="domainText"
        type="text"
        inputmode="url"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        :maxlength="MAX_HOST_LENGTH"
        :placeholder="$t('phone.tunnel.domainPlaceholder')"
        :disabled="saving"
        :aria-invalid="domainIssue !== null"
        :aria-describedby="domainIssue !== null ? 'tunnel-domain-error' : undefined"
        class="h-10 w-full rounded-lg border border-line-strong bg-canvas px-3 font-mono text-sm text-ink outline-none focus:border-accent disabled:opacity-60"
        data-testid="tunnel-domain"
        @change="checkDomain"
      />
      <InlineError v-if="domainIssue === 'required'" id="tunnel-domain-error" data-testid="tunnel-domain-error">{{ $t('phone.tunnel.domainRequired') }}</InlineError>
      <InlineError v-else-if="domainIssue === 'invalid'" id="tunnel-domain-error" data-testid="tunnel-domain-error">{{ $t('phone.tunnel.domainInvalid') }}</InlineError>
    </SettingsRow>

    <SettingsRow :label="$t('phone.tunnel.token')" :description="$t('phone.tunnel.tokenHelp')" :label-for="showTokenField ? 'tunnel-token' : undefined" stacked>
      <div v-if="!showTokenField" class="flex flex-wrap items-center gap-3" data-testid="tunnel-token-set">
        <span class="text-sm text-ok">{{ $t('phone.tunnel.tokenSet') }}</span>
        <AppButton variant="outline" size="sm" :disabled="saving" data-testid="tunnel-token-replace" @click="replace">{{ $t('phone.tunnel.tokenReplace') }}</AppButton>
      </div>
      <div v-else class="flex flex-wrap items-center gap-2">
        <input
          id="tunnel-token"
          ref="tokenInput"
          v-model="tokenText"
          type="password"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          :placeholder="$t('phone.tunnel.tokenPlaceholder')"
          :disabled="saving"
          :aria-invalid="issue === 'token'"
          :aria-describedby="issue === 'token' ? 'tunnel-token-error' : undefined"
          class="h-10 min-w-0 flex-1 rounded-lg border border-line-strong bg-canvas px-3 font-mono text-sm text-ink outline-none focus:border-accent disabled:opacity-60"
          data-testid="tunnel-token"
        />
        <AppButton v-if="replacing" variant="ghost" size="lg" :disabled="saving" data-testid="tunnel-token-cancel" @click="cancelReplace">{{ $t('phone.tunnel.tokenCancel') }}</AppButton>
      </div>
      <InlineError v-if="issue === 'token'" id="tunnel-token-error" data-testid="tunnel-token-error">{{ $t('phone.tunnel.tokenRequired') }}</InlineError>
    </SettingsRow>

    <div class="flex flex-wrap items-center gap-3 py-4">
      <AppButton variant="outline" size="lg" :disabled="saving || !dirty" data-testid="tunnel-save" @click="submit()">{{ $t('phone.tunnel.save') }}</AppButton>
    </div>

    <div class="py-4">
      <ToggleSwitch :model-value="enabled" :label="$t('phone.tunnel.enable')" :description="$t('phone.tunnel.enableHelp')" :disabled="saving" data-testid="tunnel-enable" @update:model-value="setEnabled" />
      <!-- A live region that is always there, so a change of state (starting, running, error) is read out. -->
      <div class="mt-2 flex flex-col gap-1.5" role="status" data-testid="tunnel-status-area">
        <p v-if="status !== 'off'" class="flex items-center gap-2 text-xs text-ink-muted">
          <span>{{ $t('phone.tunnel.statusLabel') }}</span>
          <span
            class="rounded-full border px-2 py-0.5 text-2xs"
            :class="{
              'border-ok/50 text-ok': status === 'running',
              'border-warn/50 text-warn': status === 'starting',
              'border-fail/50 text-fail': status === 'error',
            }"
            data-testid="tunnel-status"
          >{{ $t(`phone.tunnel.status.${status}`) }}</span>
        </p>
      </div>
      <InlineError v-if="settings.tunnelProblem" class="mt-2" data-testid="tunnel-problem">{{ $t('phone.tunnel.problem', { problem: settings.tunnelProblem }) }}</InlineError>
    </div>
  </SettingsSection>
</template>
