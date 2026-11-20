<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useNewConversation } from '../composables/useNewConversation';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import IconButton from './IconButton.vue';
import InlineError from './InlineError.vue';
import ProjectPicker from './ProjectPicker.vue';

/** Starts a conversation with the user's own words: text, files and an optional project. Emits the new id once created. */
const emit = defineEmits<{ created: [id: string] }>();

const composer = useNewConversation();
const { text, projectId, files, projects, submitting, error, draftError, inactiveReason, canSubmit } = composer;
const fileInput = ref<HTMLInputElement | null>(null);
const message = ref<HTMLTextAreaElement | null>(null);

onMounted(() => message.value?.focus());

async function submit(): Promise<void> {
  const id = await composer.submit();
  if (id) emit('created', id);
}

async function onFilesChosen(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  await files.add(input.files ?? []);
  input.value = '';
}

/** Files dropped anywhere on the composer join the draft. */
function onDrop(event: DragEvent): void {
  const dropped = [...(event.dataTransfer?.files ?? [])];
  if (dropped.length > 0) void files.add(dropped);
}

defineExpose({ focus: (): void => message.value?.focus() });
</script>

<template>
  <form class="flex flex-col gap-4" :aria-busy="submitting" data-testid="new-conversation" @submit.prevent="submit" @dragover.prevent @drop.prevent="onDrop">
    <InlineError v-if="inactiveReason" box data-testid="new-conversation-inactive">{{ inactiveReason }}</InlineError>

    <div class="flex flex-col gap-1.5">
      <label for="new-conversation-message" class="text-xs font-medium text-ink-muted">{{ $t('newConversation.message') }}</label>
      <textarea
        id="new-conversation-message"
        ref="message"
        v-model="text"
        rows="6"
        :placeholder="$t('newConversation.placeholder')"
        aria-describedby="new-conversation-hint"
        :aria-invalid="draftError ? 'true' : undefined"
        class="min-h-36 w-full resize-y rounded-lg border border-line-strong bg-surface px-3 py-2 text-ink outline-none focus:border-accent"
        :class="{ 'border-fail/70': draftError }"
        data-testid="new-conversation-text"
        @keydown.meta.enter.prevent="submit"
        @keydown.ctrl.enter.prevent="submit"
        @paste="files.onPaste"
      />
      <InlineError v-if="draftError">{{ draftError }}</InlineError>
    </div>

    <div class="flex flex-col gap-2">
      <div class="flex items-center gap-2">
        <input
          ref="fileInput"
          type="file"
          multiple
          accept="image/png,image/jpeg,image/gif,image/webp,text/plain,text/markdown,application/json,.log"
          class="sr-only"
          tabindex="-1"
          :aria-label="$t('composer.choose')"
          @change="onFilesChosen"
        />
        <IconButton :label="$t('composer.add')" icon="plus" bordered :disabled="files.uploading.value || submitting" @click="fileInput?.click()" />
        <span class="text-xs text-ink-dim">{{ files.uploading.value ? $t('newConversation.uploading') : $t('newConversation.filesHint') }}</span>
      </div>
      <InlineError v-if="files.error.value">{{ files.error.value }}</InlineError>
      <ul v-if="files.files.value.length > 0" class="flex flex-wrap gap-2" :aria-label="$t('composer.filesLabel')">
        <li v-for="file in files.files.value" :key="file.ref.id" class="relative">
          <img v-if="file.preview" :src="file.preview" :alt="file.ref.name" class="size-14 rounded-md border border-line object-cover" />
          <span v-else class="flex h-14 max-w-40 items-center rounded-md border border-line px-2 text-xs text-ink-muted">
            <span class="truncate">{{ file.ref.name }}</span>
          </span>
          <button
            type="button"
            class="absolute -right-1.5 -top-1.5 flex size-5 items-center justify-center rounded-full border border-line-strong bg-surface text-2xs text-ink-muted hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
            :aria-label="$t('composer.remove', { name: file.ref.name })"
            @click="files.remove(file.ref.id)"
          >
            &#10005;
          </button>
        </li>
      </ul>
    </div>

    <ProjectPicker v-model="projectId" :state="projects" :disabled="submitting" @retry="composer.loadProjects" />

    <p class="flex items-start gap-1.5 text-xs leading-relaxed text-ink-muted">
      <AppIcon name="info" size="sm" class="mt-0.5" />
      <span>{{ $t('newConversation.trust') }}</span>
    </p>

    <InlineError v-if="error" box data-testid="new-conversation-error">{{ error }}</InlineError>

    <div class="flex flex-wrap items-center gap-3">
      <AppButton type="submit" variant="primary" size="lg" :disabled="!canSubmit" data-testid="new-conversation-submit">
        <AppIcon name="send" size="sm" />
        {{ submitting ? $t('newConversation.sending') : $t('newConversation.send') }}
      </AppButton>
      <span id="new-conversation-hint" class="text-xs text-ink-dim">{{ $t('newConversation.shortcutHint') }}</span>
    </div>
  </form>
</template>
