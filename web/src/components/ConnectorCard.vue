<script setup lang="ts">
import { ref } from 'vue';
import { lang } from '../i18n';
import type { ConnectorStatus } from '../api/types';
import { relativeTime } from '../lib/format';
import { hasSettings } from '../lib/connectorSettings';
import { benefitKey } from '../lib/optionalConnectors';
import AppButton from './AppButton.vue';
import ProfileAvatar from './ProfileAvatar.vue';

defineProps<{ connector: ConnectorStatus; busy: boolean }>();
defineEmits<{ reconnect: []; disconnect: []; settings: [] }>();

const confirming = ref(false);
</script>

<template>
  <article class="flex flex-col gap-4 rounded-xl border border-line bg-surface p-4" :aria-label="connector.descriptor.name" data-testid="connector-card">
    <div class="flex items-start gap-3">
      <ProfileAvatar :name="connector.profile?.name ?? connector.descriptor.name" :url="connector.profile?.avatarUrl" />
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <h2 class="text-base font-semibold text-ink">{{ connector.descriptor.name }}</h2>
          <span
            class="rounded-full px-2 py-0.5 text-2xs font-medium"
            :class="connector.connected ? 'bg-ok/15 text-ok' : 'bg-warn/15 text-warn'"
          >
            {{ connector.connected ? $t('connectors.card.connected') : $t('connectors.card.notConnected') }}
          </span>
          <span v-if="connector.descriptor.required" class="text-2xs text-ink-dim">{{ $t('connectors.card.required') }}</span>
        </div>
        <p class="text-xs text-ink-dim">{{ connector.descriptor.tagline }}</p>
      </div>
    </div>
    <p v-if="!connector.descriptor.required && !connector.connected" class="text-xs leading-relaxed text-ink-muted">{{ $t(benefitKey(connector.descriptor.id)) }}</p>

    <div v-if="connector.connected && connector.profile" class="rounded-lg bg-canvas px-3 py-2.5">
      <p class="text-sm text-ink">{{ connector.profile.name }}</p>
      <p v-if="connector.profile.subtitle" class="text-xs text-ink-muted">{{ connector.profile.subtitle }}</p>
      <ul v-if="(connector.profile.facts ?? []).length > 0" class="mt-2 flex flex-col gap-0.5 text-xs text-ink-dim">
        <li v-for="fact in connector.profile.facts ?? []" :key="fact.label" class="flex justify-between gap-3">
          <span>{{ fact.label }}</span>
          <span class="truncate text-ink-muted">{{ fact.value }}</span>
        </li>
      </ul>
      <p v-if="(connector.profile.warnings ?? []).length > 0" class="mt-2 text-xs text-warn">
        {{ $t('connectors.card.warnings', { n: (connector.profile.warnings ?? []).length, titles: (connector.profile.warnings ?? []).map((w) => w.title).join(', ') }, (connector.profile.warnings ?? []).length) }}
      </p>
      <p v-if="connector.connectedAt" class="mt-2 text-2xs text-ink-dim">{{ $t('connectors.card.connectedAt', { time: relativeTime(connector.connectedAt, new Date(), lang) }) }}</p>
    </div>

    <div class="mt-auto flex flex-wrap items-center gap-2">
      <template v-if="!confirming">
        <AppButton variant="outline" size="sm" :disabled="busy" @click="$emit('reconnect')">{{ connector.connected ? $t('connectors.card.reconnectBtn') : $t('connectors.card.connectBtn') }}</AppButton>
        <AppButton v-if="connector.connected && hasSettings(connector.descriptor.id)" variant="outline" size="sm" :disabled="busy" data-testid="connector-settings" @click="$emit('settings')">{{ $t('connectors.card.settings') }}</AppButton>
        <AppButton v-if="connector.connected" variant="ghost" size="sm" :disabled="busy" @click="confirming = true">{{ $t('connectors.card.disconnect') }}</AppButton>
      </template>
      <template v-else>
        <p class="w-full text-xs text-ink-muted" role="alert">
          {{ $t('connectors.card.confirmDisconnect', { name: connector.descriptor.name }) }}
          <template v-if="connector.descriptor.required">{{ $t('connectors.card.confirmRequired') }}</template>
        </p>
        <AppButton variant="danger" size="sm" :disabled="busy" data-testid="confirm-disconnect" @click="(confirming = false), $emit('disconnect')">{{ $t('connectors.card.yes') }}</AppButton>
        <AppButton variant="ghost" size="sm" @click="confirming = false">{{ $t('common.cancel') }}</AppButton>
      </template>
    </div>
  </article>
</template>
