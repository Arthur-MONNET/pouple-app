<script setup lang="ts">
import type { PhoneAccess } from '../composables/usePhoneAccess';
import AppIcon from './AppIcon.vue';
import PhoneAddress from './PhoneAddress.vue';
import PhoneExternal from './PhoneExternal.vue';
import PhoneNetwork from './PhoneNetwork.vue';
import PhoneTunnel from './PhoneTunnel.vue';

defineProps<{ access: PhoneAccess }>();
</script>

<template>
  <!-- Closed by default: the technical settings are out of the way until someone looks for them. -->
  <details class="group/advanced rounded-xl border border-line bg-surface" data-testid="phone-advanced">
    <summary class="flex cursor-pointer list-none items-center gap-2 rounded-xl px-4 py-3 text-sm font-medium text-ink hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent">
      <AppIcon name="chevron-right" size="sm" class="shrink-0 transition-transform group-open/advanced:rotate-90 motion-reduce:transition-none" />
      <span>{{ $t('phone.advanced.title') }}</span>
      <span class="min-w-0 truncate text-xs font-normal text-ink-muted">{{ $t('phone.advanced.hint') }}</span>
    </summary>
    <div class="flex flex-col gap-8 border-t border-line px-4 py-5">
      <PhoneNetwork :access="access" />
      <PhoneExternal :access="access" />
      <PhoneAddress :access="access" />
      <PhoneTunnel :access="access" />
    </div>
  </details>
</template>
