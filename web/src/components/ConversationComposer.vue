<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { errorMessage } from '../api/client';
import type { AssistantRequest } from '../api/types';
import { useAssistantStore } from '../composables/assistantStore';
import { useComposerFiles } from '../composables/composerFiles';
import { isWorking, requestTitle } from '../lib/assistantRequests';
import AppButton from './AppButton.vue';
import IconButton from './IconButton.vue';

const props = defineProps<{ request: AssistantRequest }>();
const store = useAssistantStore();

const draft = ref('');
const sendError = ref<string | null>(null);
const sending = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);

const working = computed(() => isWorking(props.request));
/** A question or a proposal is answered with its own card, not with a free message. */
const canWrite = computed(() => !working.value && props.request.status !== 'awaiting-user');
const files = useComposerFiles(computed(() => (canWrite.value ? props.request.id : null)));
const canSend = computed(() => canWrite.value && !sending.value && !files.uploading.value && (draft.value.trim() !== '' || files.files.value.length > 0));
const placeholder = computed(() => (working.value ? 'Working…' : canWrite.value ? 'Ask a follow-up…' : 'Answer the question above to continue'));

watch(
  () => props.request.id,
  () => {
    draft.value = '';
    sendError.value = null;
  },
);

async function send(): Promise<void> {
  if (!canSend.value) return;
  sending.value = true;
  sendError.value = null;
  try {
    await store.send(props.request.id, draft.value.trim(), files.files.value.map((file) => file.ref.id));
    draft.value = '';
    files.forget();
  } catch (cause) {
    sendError.value = errorMessage(cause);
  } finally {
    sending.value = false;
  }
}

async function stop(): Promise<void> {
  sendError.value = null;
  try {
    await store.stopRequest(props.request.id);
  } catch (cause) {
    sendError.value = errorMessage(cause);
  }
}

async function onFilesChosen(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  await files.add(input.files ?? []);
  input.value = '';
}
</script>

<template>
  <form class="px-4 py-3" @submit.prevent="send" @dragover.prevent @drop.prevent="files.onDrop">
    <p class="mb-1.5 truncate text-2xs text-ink-dim">About: {{ requestTitle(request) }}</p>
    <p v-if="sendError || files.error.value" role="alert" class="mb-1.5 text-xs text-fail">{{ sendError ?? files.error.value }}</p>
    <ul v-if="files.files.value.length > 0" class="mb-2 flex flex-wrap gap-2" aria-label="Files to send">
      <li v-for="file in files.files.value" :key="file.ref.id" class="relative">
        <img v-if="file.preview" :src="file.preview" :alt="file.ref.name" class="size-14 rounded-md border border-line object-cover" />
        <span v-else class="flex h-14 max-w-32 items-center rounded-md border border-line px-2 text-xs text-ink-muted">
          <span class="truncate">{{ file.ref.name }}</span>
        </span>
        <button
          type="button"
          class="absolute -right-1.5 -top-1.5 flex size-5 items-center justify-center rounded-full border border-line-strong bg-surface text-2xs text-ink-muted hover:text-ink"
          :aria-label="`Remove ${file.ref.name}`"
          @click="files.remove(file.ref.id)"
        >
          &#10005;
        </button>
      </li>
    </ul>
    <div class="flex items-end gap-2">
      <input
        ref="fileInput"
        type="file"
        multiple
        accept="image/png,image/jpeg,image/gif,image/webp,text/plain,text/markdown,application/json,.log"
        class="sr-only"
        tabindex="-1"
        aria-label="Choose files to add"
        @change="onFilesChosen"
      />
      <IconButton label="Add a photo or a file" icon="plus" bordered :disabled="!canWrite || files.uploading.value" @click="fileInput?.click()" />
      <textarea
        v-model="draft"
        rows="1"
        maxlength="4000"
        :disabled="!canWrite"
        aria-label="Message to the assistant"
        :placeholder="placeholder"
        class="max-h-28 min-h-8 flex-1 resize-none rounded-md border border-line-strong bg-surface px-2.5 py-1.5 text-ink outline-none focus:border-accent disabled:opacity-60"
        @keydown.enter.exact.prevent="send"
        @paste="files.onPaste"
      />
      <AppButton v-if="working" type="button" variant="danger" aria-label="Stop the assistant" @click="stop">Stop</AppButton>
      <AppButton v-else type="submit" variant="primary" :disabled="!canSend">Send</AppButton>
    </div>
  </form>
</template>
