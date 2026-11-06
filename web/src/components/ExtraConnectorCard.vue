<script setup lang="ts">
import type { ExtraCard } from '../lib/optionalConnectors';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import ProfileAvatar from './ProfileAvatar.vue';

defineProps<{ card: ExtraCard }>();
defineEmits<{ connect: [] }>();
</script>

<template>
  <article
    class="flex h-full flex-col gap-3 rounded-xl border bg-surface p-4"
    :class="card.connected ? 'border-ok/40' : 'border-line'"
    :aria-label="card.name"
    data-testid="extra-card"
  >
    <div class="flex items-start gap-3">
      <ProfileAvatar :name="card.name" :url="card.avatarUrl ?? undefined" />
      <div class="min-w-0 flex-1">
        <h3 class="text-base font-semibold text-ink">{{ card.name }}</h3>
        <p class="text-xs text-ink-dim">{{ card.tagline }}</p>
      </div>
      <span v-if="card.connected" class="flex size-6 shrink-0 items-center justify-center rounded-full border border-ok bg-ok/15 text-ok" data-testid="extra-check">
        <AppIcon name="check" size="sm" />
        <span class="sr-only">{{ $t('connectors.card.connected') }}</span>
      </span>
    </div>
    <p class="text-xs leading-relaxed text-ink-muted">{{ $t(card.benefitKey) }}</p>
    <div class="mt-auto">
      <p v-if="card.connected" class="truncate text-sm text-ok" data-testid="extra-profile">{{ $t('extras.connectedAs', { name: card.profileName ?? card.name }) }}</p>
      <AppButton v-else variant="outline" size="md" :aria-label="$t('extras.connectNamed', { name: card.name })" data-testid="extra-connect" @click="$emit('connect')">{{ $t('connectors.card.connectBtn') }}</AppButton>
    </div>
  </article>
</template>
