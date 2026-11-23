<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import type { PhoneAccess } from '../composables/usePhoneAccess';
import { MAX_HOST_LENGTH, checkTunnelDomain, tunnelPatch } from '../lib/phonePairing';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import InlineError from './InlineError.vue';
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
  <div v-if="settings" class="flex flex-col gap-4 rounded-2xl border border-line-strong bg-surface p-5" data-testid="phone-tunnel">
    <ToggleSwitch :model-value="enabled" :label="$t('phone.tunnel.title')" :description="$t('phone.tunnel.intro')" :disabled="saving" data-testid="tunnel-enable" @update:model-value="setEnabled">
      <template #badge>
        <!-- A live region that is always there, so a change of state (starting, running, error) is read out. -->
        <span role="status" data-testid="tunnel-status-area">
          <span
            v-if="status !== 'off'"
            class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold"
            :class="{
              'bg-ok/15 text-ok': status === 'running',
              'bg-warn/15 text-warn': status === 'starting',
              'bg-fail/15 text-fail': status === 'error',
            }"
            data-testid="tunnel-status"
          >
            <span
              class="size-1.5 rounded-full"
              :class="{ 'bg-ok': status === 'running', 'bg-warn motion-safe:animate-pulse': status === 'starting', 'bg-fail': status === 'error' }"
              aria-hidden="true"
            />{{ $t(`phone.tunnel.status.${status}`) }}
          </span>
        </span>
      </template>
    </ToggleSwitch>
    <InlineError v-if="settings.tunnelProblem" data-testid="tunnel-problem">{{ $t('phone.tunnel.problem', { problem: settings.tunnelProblem }) }}</InlineError>

    <ol class="flex flex-col gap-2 border-t border-line pt-3.5 text-xs leading-relaxed text-ink-muted" data-testid="tunnel-steps">
      <li class="flex gap-2.5"><span class="font-mono text-accent" aria-hidden="true">1</span><span>{{ $t('phone.tunnel.step1') }}</span></li>
      <li class="flex gap-2.5"><span class="font-mono text-accent" aria-hidden="true">2</span><span>{{ $t('phone.tunnel.step2') }}</span></li>
      <li class="flex gap-2.5"><span class="font-mono text-accent" aria-hidden="true">3</span><span>{{ $t('phone.tunnel.step3') }}</span></li>
    </ol>

    <div class="flex flex-col gap-3">
      <div class="flex flex-col gap-1.5">
        <label for="tunnel-domain" class="text-xs font-medium text-ink">{{ $t('phone.tunnel.domain') }}</label>
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
      </div>

      <div class="flex flex-col gap-1.5">
        <label :for="showTokenField ? 'tunnel-token' : undefined" class="text-xs font-medium text-ink">{{ $t('phone.tunnel.token') }}</label>
        <div v-if="!showTokenField" class="flex h-10 items-center gap-3 rounded-lg border border-line-strong bg-canvas px-3 text-sm" data-testid="tunnel-token-set">
          <span class="inline-flex items-center gap-2 text-ok"><AppIcon name="check" size="sm" />{{ $t('phone.tunnel.tokenSet') }}</span>
          <button type="button" class="ml-auto rounded py-1 text-xs font-medium text-ink hover:text-accent focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-50" :disabled="saving" data-testid="tunnel-token-replace" @click="replace">{{ $t('phone.tunnel.tokenReplace') }}</button>
        </div>
        <div v-else class="flex items-center gap-2">
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
      </div>

      <div v-if="dirty"><AppButton variant="primary" size="lg" :disabled="saving" data-testid="tunnel-save" @click="submit()">{{ $t('phone.tunnel.save') }}</AppButton></div>
    </div>

    <div class="flex items-start gap-3 rounded-xl border border-lavender/30 bg-lavender/10 px-3.5 py-3" data-testid="tunnel-privacy">
      <AppIcon name="lock" size="sm" class="mt-0.5 shrink-0 text-lavender" />
      <p class="text-xs leading-relaxed text-ink">{{ $t('phone.tunnel.privacy') }}</p>
    </div>
  </div>
</template>
