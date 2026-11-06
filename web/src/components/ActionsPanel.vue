<script setup lang="ts">
import { computed } from 'vue';
import type { ActionInfo } from '../api/types';
import type { ChecklistItem } from '../lib/connectorFlow';
import { useActionRun } from '../composables/useActionRun';
import ActionLog from './ActionLog.vue';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import AppSpinner from './AppSpinner.vue';
import PouMascot from './PouMascot.vue';

const props = defineProps<{ connectorId: string; actions: ActionInfo[]; checklist: ChecklistItem[]; message: string | null }>();
/** An action ended well: the step must look again at what is in place. */
const emit = defineEmits<{ finished: [] }>();

const run = useActionRun();
const open = (url: string): void => void window.open(url, '_blank', 'noopener');

/** The action that fixes the first unmet item is the one to press next. */
const nextAction = computed(() => props.checklist.find((item) => !item.ok)?.actionId ?? null);

async function start(action: ActionInfo): Promise<void> {
  if (await run.run(props.connectorId, action.id)) emit('finished');
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <ul v-if="checklist.length > 0" class="divide-y divide-line-soft rounded-lg border border-line bg-surface" aria-label="What is in place">
      <li v-for="item in checklist" :key="item.id" class="flex items-center gap-3 px-3 py-2.5">
        <span
          class="flex size-5 shrink-0 items-center justify-center rounded-full"
          :class="item.ok ? 'bg-ok/20 text-ok' : 'bg-fail/15 text-fail'"
          aria-hidden="true"
        >
          <AppIcon :name="item.ok ? 'check' : 'close'" size="sm" />
        </span>
        <span class="text-sm text-ink">{{ item.label }}</span>
        <span class="ml-auto text-xs" :class="item.ok ? 'text-ok' : 'text-ink-dim'">{{ item.ok ? 'Done' : 'To do' }}</span>
      </li>
    </ul>

    <p v-if="message" role="status" class="text-sm text-ink-muted" data-testid="check-message">{{ message }}</p>

    <div class="flex flex-wrap gap-2">
      <AppButton
        v-for="action in actions"
        :key="action.id"
        :variant="action.id === nextAction ? 'primary' : 'outline'"
        size="lg"
        :disabled="run.running.value !== null"
        :data-testid="`action-${action.id}`"
        @click="start(action)"
      >
        <AppSpinner v-if="run.running.value === action.id" />
        {{ action.label }}
      </AppButton>
    </div>

    <div aria-live="polite" class="flex flex-col gap-3">
      <div v-if="run.running.value" class="flex items-center gap-3 rounded-xl bg-lavender px-4 py-2 text-navy">
        <PouMascot pose="investigate" class="size-16 shrink-0" />
        <p class="flex items-center gap-2 text-sm font-medium">
          <AppSpinner />
          {{ run.running.value === 'login' ? 'Waiting for you to sign in in the browser…' : 'Installing. This takes a minute.' }}
        </p>
      </div>
      <p v-if="run.openUrl.value" class="text-sm text-ink-muted">
        The sign-in page should have opened.
        <button type="button" class="text-accent underline-offset-2 hover:underline" @click="open(run.openUrl.value)">Open the sign-in page</button>
      </p>
      <p v-if="run.error.value" role="alert" class="flex items-start gap-1.5 text-sm text-fail">
        <AppIcon name="alert" />
        <span>{{ run.error.value }}</span>
      </p>
    </div>

    <ActionLog :lines="run.lines.value" :running="run.running.value !== null" />
  </div>
</template>
