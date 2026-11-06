<script setup lang="ts">
import { computed, ref } from 'vue';
import { formatDay, relativeTime } from '../lib/format';
import { MANUAL_COMMANDS, shortCommit, splitChanges } from '../lib/update';
import { useUpdate } from '../composables/useUpdate';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import AppSpinner from './AppSpinner.vue';
import PouMascot from './PouMascot.vue';
import UpdateConfirmDialog from './UpdateConfirmDialog.vue';

const update = useUpdate();
const { info, view, error, checking, closing, canLaunch } = update;

const confirming = ref(false);
const copied = ref<string | null>(null);

const installed = computed(() => info.value?.installed ?? null);
const changes = computed(() => (info.value ? splitChanges(info.value) : { shown: [], hidden: 0 }));
const showManual = computed(() => !canLaunch.value && (view.value === 'unsupported' || view.value === 'available'));

function confirm(): void {
  update.launch();
}

function cancel(): void {
  confirming.value = false;
}

async function copy(command: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(command);
    copied.value = command;
    setTimeout(() => (copied.value = null), 2000);
  } catch {
    // Without clipboard access the command stays selectable on screen.
  }
}
</script>

<template>
  <section id="updates" aria-labelledby="updates-title" class="flex scroll-mt-6 flex-col gap-4 rounded-xl border border-line bg-surface p-5" data-testid="update-section">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h2 id="updates-title" class="text-base font-semibold text-ink">{{ $t('update.title') }}</h2>
        <p v-if="installed" class="text-xs text-ink-muted" data-testid="update-version">
          {{ $t('update.version', { version: installed.version, commit: shortCommit(installed.commit) }) }}
          <template v-if="formatDay(installed.installedAt)"> · {{ $t('update.installedOn', { date: formatDay(installed.installedAt) }) }}</template>
        </p>
        <p v-if="info && view !== 'unsupported'" class="text-xs text-ink-dim" data-testid="update-last-check">
          {{ info.checkedAt ? $t('update.lastCheck', { when: relativeTime(info.checkedAt) }) : $t('update.neverChecked') }}
        </p>
      </div>
      <AppButton v-if="view !== 'unsupported'" variant="outline" :disabled="checking || view === 'checking'" data-testid="update-check" @click="update.check()">
        <AppSpinner v-if="checking || view === 'checking'" />
        <AppIcon v-else name="refresh" size="sm" />
        {{ checking || view === 'checking' ? $t('update.checking') : $t('update.check') }}
      </AppButton>
    </div>

    <p v-if="view === 'checking'" role="status" class="text-sm text-ink-muted">{{ $t('update.checkingHelp') }}</p>

    <p v-if="error && !checking" role="status" class="flex items-start gap-1.5 rounded-lg border border-line-strong bg-raised px-3 py-2 text-sm text-ink-muted" data-testid="update-error">
      <AppIcon name="info" size="sm" class="mt-0.5" />
      <span class="min-w-0">{{ $t('update.checkError', { error }) }}</span>
    </p>

    <div v-if="view === 'unsupported' && info" class="flex flex-col gap-2 rounded-lg border border-line-strong bg-raised p-4" data-testid="update-unsupported">
      <h3 class="text-sm font-semibold text-ink">{{ $t('update.unsupportedTitle') }}</h3>
      <p class="text-sm text-ink-muted">{{ info.message }}</p>
    </div>

    <div v-if="view === 'upToDate'" class="flex items-center gap-4 rounded-lg border border-line-soft bg-canvas p-4" data-testid="update-uptodate">
      <PouMascot pose="sleep" class="size-16" />
      <div>
        <p class="text-sm font-semibold text-ink">{{ $t('update.upToDate') }}</p>
        <p class="text-xs text-ink-muted">{{ $t('update.upToDateHelp') }}</p>
      </div>
    </div>

    <div v-if="view === 'available' && info" class="flex flex-col gap-4 rounded-lg border border-accent/50 bg-accent/5 p-4" data-testid="update-available">
      <div>
        <h3 class="text-sm font-semibold text-ink">{{ $t('update.availableTitle', info.behind || info.changes.length) }}</h3>
        <p class="text-xs text-ink-muted">{{ $t('update.availableHelp', { from: shortCommit(info.installed.commit), to: shortCommit(info.latest.commit) }) }}</p>
      </div>

      <div v-if="changes.shown.length > 0" class="flex flex-col gap-1.5">
        <p class="text-xs font-medium text-ink-muted">{{ $t('update.changesLabel') }}</p>
        <ul class="flex list-disc flex-col gap-1 pl-5 text-sm text-ink marker:text-ink-dim">
          <li v-for="(subject, index) in changes.shown" :key="index" class="break-words">{{ subject }}</li>
        </ul>
        <p v-if="changes.hidden > 0" class="text-xs text-ink-dim">{{ $t('update.moreChanges', changes.hidden) }}</p>
      </div>

      <div v-if="info.blockers.length > 0" class="flex flex-col gap-2 rounded-lg border border-warn/50 bg-warn/10 p-3" data-testid="update-blockers">
        <p class="flex items-center gap-1.5 text-sm font-medium text-warn"><AppIcon name="alert" size="sm" />{{ $t('update.blockersTitle') }}</p>
        <ul class="flex flex-col gap-2">
          <li v-for="blocker in info.blockers" :key="blocker.code" class="text-sm text-ink">
            {{ blocker.message }}
            <span class="block text-xs text-ink-muted">{{ $t(`update.blockerHint.${blocker.code}`) }}</span>
          </li>
        </ul>
      </div>

      <AppButton v-if="canLaunch" variant="primary" size="lg" class="w-fit" data-testid="update-now" @click="confirming = true">{{ $t('update.now') }}</AppButton>
    </div>

    <div v-if="showManual" class="flex flex-col gap-2" data-testid="update-manual">
      <h3 class="text-sm font-medium text-ink">{{ $t('update.manualTitle') }}</h3>
      <p class="text-xs text-ink-muted">{{ $t('update.manualHelp') }}</p>
      <ul class="flex flex-col gap-2">
        <li v-for="command in MANUAL_COMMANDS" :key="command" class="flex items-center gap-2">
          <code class="min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded-lg border border-line-strong bg-canvas px-3 py-2 font-mono text-xs text-ink">{{ command }}</code>
          <AppButton size="sm" variant="outline" :aria-label="$t('update.copyAria', { command })" @click="copy(command)">
            <AppIcon :name="copied === command ? 'check' : 'paste'" size="sm" />
            <span :aria-live="copied === command ? 'polite' : 'off'">{{ copied === command ? $t('update.copied') : $t('update.copy') }}</span>
          </AppButton>
        </li>
      </ul>
    </div>

    <UpdateConfirmDialog v-if="confirming || closing" :closing="closing" @cancel="cancel" @confirm="confirm" />
  </section>
</template>
