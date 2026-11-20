<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';
import { errorMessage } from '../api/client';
import type { AlertInput } from '../api/types';
import AlertForm from '../components/AlertForm.vue';
import AlertRow from '../components/AlertRow.vue';
import AppIcon from '../components/AppIcon.vue';
import InlineError from '../components/InlineError.vue';
import { useAssistantStore } from '../composables/assistantStore';
import { groupByDay } from '../lib/alerts';

const router = useRouter();
const store = useAssistantStore();
const form = ref<InstanceType<typeof AlertForm> | null>(null);
const busy = ref(false);
const actionError = ref<string | null>(null);

const days = computed(() => groupByDay(store.alerts.value));
/** An alert made from a conversation links back to it, as long as that conversation still exists. */
const hasConversation = (requestId: string | undefined): boolean => requestId !== undefined && store.byId(requestId) !== null;


async function guard(action: () => Promise<unknown>): Promise<boolean> {
  busy.value = true;
  actionError.value = null;
  try {
    await action();
    return true;
  } catch (cause) {
    actionError.value = errorMessage(cause);
    return false;
  } finally {
    busy.value = false;
  }
}

async function create(input: AlertInput): Promise<void> {
  if (await guard(() => store.createAlert(input))) form.value?.reset();
}

const open = (requestId: string | undefined): void => void (requestId && router.push({ name: 'conversation', params: { id: requestId } }));
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto">
    <div class="mx-auto flex max-w-2xl flex-col gap-6 px-5 py-6">
      <header>
        <h1 class="flex items-center gap-2 text-title font-semibold text-ink"><AppIcon name="bell" size="lg" />{{ $t('alerts.title') }}</h1>
        <p class="text-sm text-ink-muted">{{ $t('alerts.intro') }}</p>
      </header>

      <InlineError v-if="store.error.value" box>{{ $t('home.coreDown', { error: store.error.value }) }}</InlineError>
      <InlineError v-if="actionError" box>{{ actionError }}</InlineError>

      <section v-if="store.loaded.value && days.length === 0" class="rounded-xl border border-dashed border-line px-4 py-8 text-center" data-testid="alerts-empty">
        <p class="font-medium text-ink">{{ $t('alerts.emptyTitle') }}</p>
        <p class="mt-1 text-sm text-ink-muted">{{ $t('alerts.emptyBody') }}</p>
      </section>

      <section v-for="day in days" :key="day.key" :aria-label="day.label" data-testid="alert-day">
        <h2 class="text-2xs font-medium uppercase tracking-wider text-ink-dim">{{ day.label }}</h2>
        <ul class="mt-1 flex flex-col divide-y divide-line border-y border-line" :aria-label="$t('alerts.listLabel')">
          <AlertRow
            v-for="alert in day.alerts"
            :key="alert.id"
            :alert="alert"
            :busy="busy"
            :has-conversation="hasConversation(alert.requestId)"
            @update="(patch) => guard(() => store.updateAlert(alert.id, patch))"
            @remove="guard(() => store.deleteAlert(alert.id))"
            @open="open(alert.requestId)"
          />
        </ul>
      </section>

      <AlertForm ref="form" :busy="busy" @submit="create" />
    </div>
  </main>
</template>
