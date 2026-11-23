<script setup lang="ts">
import type { PhoneAccess } from '../composables/usePhoneAccess';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';
import PairingPayload from './PairingPayload.vue';
import SettingsSection from './SettingsSection.vue';

defineProps<{ access: PhoneAccess }>();
</script>

<template>
  <SettingsSection v-if="access.settings.value?.enabled && access.settings.value.externalEnabled" :title="$t('phone.address.title')" :description="$t('phone.address.intro')">
    <div class="flex flex-col gap-3 py-4" data-testid="phone-address">
      <p v-if="!access.canShareAddress.value && !access.address.value" class="text-sm text-ink-muted" data-testid="address-unavailable">{{ $t('phone.address.unavailable') }}</p>
      <div v-if="!access.address.value" class="flex flex-wrap gap-2">
        <AppButton variant="outline" size="lg" :disabled="!access.canShareAddress.value || access.addressLoading.value" data-testid="address-show" @click="access.showAddress()">{{ $t('phone.address.show') }}</AppButton>
      </div>
      <p v-if="access.addressLoading.value" role="status" class="flex items-center gap-2 text-sm text-ink-muted" data-testid="address-loading"><AppSpinner />{{ $t('phone.address.loading') }}</p>
      <template v-if="access.address.value">
        <PairingPayload :payload="access.address.value.payload" :label="$t('phone.address.qrLabel')" test-prefix="address" />
        <div><AppButton variant="outline" data-testid="address-hide" @click="access.hideAddress()">{{ $t('phone.address.hide') }}</AppButton></div>
      </template>
      <InlineError v-if="access.addressError.value" box data-testid="address-error">{{ $t('phone.address.error', { error: access.addressError.value }) }}</InlineError>
    </div>
  </SettingsSection>
</template>
