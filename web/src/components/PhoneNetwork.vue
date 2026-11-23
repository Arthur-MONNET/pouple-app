<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { PhoneAccess } from '../composables/usePhoneAccess';
import { MAX_PORT, MIN_PORT, interfaceChoices, isValidPort, toggleInterface } from '../lib/phonePairing';
import InlineError from './InlineError.vue';
import SettingsRow from './SettingsRow.vue';
import SettingsSection from './SettingsSection.vue';

const props = defineProps<{ access: PhoneAccess }>();
const { settings, saving } = props.access;

const choices = computed(() => (settings.value ? interfaceChoices(settings.value) : []));
const enabled = computed(() => settings.value?.enabled === true);
const automatic = computed(() => (settings.value?.interfaces.length ?? 0) === 0);
const portText = ref('');
const portError = ref(false);
watch(() => settings.value?.port, (port) => {
  if (port !== undefined) portText.value = String(port);
}, { immediate: true });

function setInterface(name: string, on: boolean): void {
  if (!settings.value) return;
  void props.access.save({ interfaces: toggleInterface(settings.value.interfaces, name, on) });
}

async function savePort(): Promise<void> {
  const port = Number(portText.value);
  portError.value = !isValidPort(port);
  if (portError.value || port === settings.value?.port) return;
  if (!(await props.access.save({ port }))) portText.value = String(settings.value?.port ?? '');
}
</script>

<template>
  <SettingsSection v-if="settings" :title="$t('phone.network.title')">
    <SettingsRow :label="$t('phone.network.label')" :description="automatic ? $t('phone.network.automatic') : $t('phone.network.chosen')" stacked>
      <p v-if="choices.length === 0" class="text-sm text-ink-muted" data-testid="phone-no-network">{{ $t('phone.network.none') }}</p>
      <ul v-else class="flex flex-col gap-2" role="group" :aria-label="$t('phone.network.label')">
        <li v-for="item in choices" :key="item.name">
          <label class="flex items-start gap-2.5 rounded-lg border border-line px-3 py-2 text-sm text-ink" :class="{ 'opacity-60': saving }">
            <input
              type="checkbox"
              class="mt-0.5 size-4 shrink-0 accent-accent"
              :checked="item.selected"
              :disabled="saving"
              :data-testid="`phone-interface-${item.name}`"
              @change="setInterface(item.name, ($event.target as HTMLInputElement).checked)"
            />
            <span class="min-w-0">
              <span class="font-medium">{{ item.name }}</span>
              <span v-if="item.loopback" class="ml-2 text-xs text-ink-muted">{{ $t('phone.network.loopback') }}</span>
              <span v-if="item.missing" class="ml-2 text-xs text-warn">{{ $t('phone.network.missing') }}</span>
              <span v-if="item.addresses.length > 0" class="block break-all font-mono text-xs text-ink-muted">{{ item.addresses.join(', ') }}</span>
            </span>
          </label>
        </li>
      </ul>
    </SettingsRow>

    <SettingsRow :label="$t('phone.port.label')" :description="$t('phone.port.help', { min: MIN_PORT, max: MAX_PORT })" label-for="phone-port">
      <input
        id="phone-port"
        v-model="portText"
        type="number"
        inputmode="numeric"
        :min="MIN_PORT"
        :max="MAX_PORT"
        step="1"
        :disabled="saving"
        :aria-invalid="portError"
        class="h-10 w-28 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent disabled:opacity-60"
        data-testid="phone-port"
        @change="savePort"
      />
      <InlineError v-if="portError" data-testid="phone-port-error">{{ $t('phone.port.invalid', { min: MIN_PORT, max: MAX_PORT }) }}</InlineError>
    </SettingsRow>

    <template #footer>
      <p v-if="enabled && settings.listening.length > 0" class="break-all py-3 text-xs text-ink-muted" data-testid="phone-listening">{{ $t('phone.network.listening', { addresses: settings.listening.join(', ') }) }}</p>
    </template>
  </SettingsSection>
</template>
