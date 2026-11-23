<script setup lang="ts">
import type { PhoneAccess } from '../composables/usePhoneAccess';
import AppIcon from './AppIcon.vue';
import PhoneAddress from './PhoneAddress.vue';
import PhoneExternal from './PhoneExternal.vue';
import PhoneFold from './PhoneFold.vue';
import PhoneNetwork from './PhoneNetwork.vue';
import PhoneTunnel from './PhoneTunnel.vue';

defineProps<{ access: PhoneAccess }>();
</script>

<template>
  <!-- Closed by default: the technical settings are out of the way until someone looks for them. -->
  <details class="group/advanced border-t border-line pt-4" data-testid="phone-advanced">
    <summary class="flex cursor-pointer list-none items-center gap-2.5 rounded text-ink focus-visible:outline-2 focus-visible:outline-accent">
      <AppIcon name="chevron-right" size="sm" class="shrink-0 text-ink-muted transition-transform group-open/advanced:rotate-90 motion-reduce:transition-none" />
      <span class="text-sm font-medium">{{ $t('phone.advanced.title') }}</span>
      <span class="min-w-0 truncate text-xs text-ink-dim">{{ $t('phone.advanced.hint') }}</span>
    </summary>

    <div class="mt-6 flex flex-col gap-8">
      <section class="flex flex-col gap-3" aria-labelledby="advanced-home">
        <h2 id="advanced-home" class="text-base font-semibold text-ink">{{ $t('phone.reach.home') }}</h2>
        <PhoneNetwork :access="access" />
      </section>

      <section class="flex flex-col gap-3" aria-labelledby="advanced-away">
        <h2 id="advanced-away" class="text-base font-semibold text-ink">{{ $t('phone.reach.away') }}</h2>
        <PhoneTunnel :access="access" />
        <PhoneFold :title="$t('phone.router.title')" :hint="$t('phone.router.hint')"><PhoneExternal :access="access" /></PhoneFold>
        <PhoneFold :title="$t('phone.address.title')" :hint="$t('phone.address.hint')"><PhoneAddress :access="access" /></PhoneFold>
      </section>
    </div>
  </details>
</template>
