<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';

const props = defineProps<{ closing: boolean }>();
const emit = defineEmits<{ cancel: []; confirm: [] }>();

const panel = ref<HTMLElement | null>(null);
const cancelButton = ref<InstanceType<typeof AppButton> | null>(null);
let previous: Element | null = null;

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Escape cancels, unless the update has been sent: the shell is already closing the app. Tab stays in the dialog. */
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.stopPropagation();
    if (!props.closing) emit('cancel');
    return;
  }
  if (event.key !== 'Tab' || !panel.value) return;
  const items = [...panel.value.querySelectorAll<HTMLElement>(FOCUSABLE)];
  const first = items[0];
  const last = items.at(-1);
  if (!first || !last) {
    event.preventDefault();
    panel.value.focus();
  } else if (event.shiftKey && document.activeElement === first) (event.preventDefault(), last.focus());
  else if (!event.shiftKey && document.activeElement === last) (event.preventDefault(), first.focus());
}

onMounted(async () => {
  previous = document.activeElement;
  await nextTick();
  // The safe choice has the focus: Enter cancels rather than quitting the app by accident.
  (cancelButton.value?.$el as HTMLElement | undefined)?.focus();
});
onBeforeUnmount(() => (previous instanceof HTMLElement ? previous.focus() : undefined));
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-40 flex items-center justify-center p-4" @keydown="onKeydown">
      <div class="absolute inset-0 bg-black/60" aria-hidden="true" @click="!closing && emit('cancel')" />
      <div
        ref="panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="update-dialog-title"
        aria-describedby="update-dialog-body"
        tabindex="-1"
        data-testid="update-dialog"
        class="relative flex max-h-full w-full max-w-md flex-col gap-4 overflow-y-auto rounded-xl border border-line-strong bg-canvas p-5 shadow-2xl outline-none"
      >
        <template v-if="closing">
          <h2 id="update-dialog-title" role="status" class="flex items-center gap-2.5 text-base font-semibold text-ink"><AppSpinner />{{ $t('update.dialog.closingTitle') }}</h2>
          <p id="update-dialog-body" class="text-sm text-ink-muted">{{ $t('update.dialog.closingHelp') }}</p>
        </template>
        <template v-else>
          <h2 id="update-dialog-title" class="text-base font-semibold text-ink">{{ $t('update.dialog.title') }}</h2>
          <div id="update-dialog-body" class="flex flex-col gap-3 text-sm text-ink-muted">
            <p>{{ $t('update.dialog.intro') }}</p>
            <ol class="flex list-decimal flex-col gap-1.5 pl-5 marker:text-ink-dim">
              <li>{{ $t('update.dialog.stepTerminal') }}</li>
              <li>{{ $t('update.dialog.stepQuit') }}</li>
              <li>{{ $t('update.dialog.stepPull') }}</li>
              <li>{{ $t('update.dialog.stepRebuild') }}</li>
            </ol>
            <p class="text-ink">{{ $t('update.dialog.kept') }}</p>
          </div>
          <div class="flex flex-wrap justify-end gap-2">
            <AppButton ref="cancelButton" variant="outline" size="lg" data-testid="update-cancel" @click="emit('cancel')">{{ $t('update.dialog.cancel') }}</AppButton>
            <AppButton variant="primary" size="lg" data-testid="update-confirm" @click="emit('confirm')">{{ $t('update.dialog.confirm') }}</AppButton>
          </div>
        </template>
      </div>
    </div>
  </Teleport>
</template>
