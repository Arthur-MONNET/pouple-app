<script setup lang="ts">
import type { ProjectsState } from '../composables/useNewConversation';
import InlineError from './InlineError.vue';

/** The optional project of a new conversation: a hint for the assistant, never a limit on what it may read. */
defineProps<{ state: ProjectsState; disabled?: boolean }>();
const model = defineModel<string | null>({ required: true });
defineEmits<{ retry: [] }>();
</script>

<template>
  <div class="flex flex-col gap-1">
    <label for="new-conversation-project" class="text-xs font-medium text-ink-muted">{{ $t('newConversation.project') }} <span class="font-normal text-ink-dim">{{ $t('common.optional') }}</span></label>
    <p v-if="state.phase === 'loading'" class="text-xs text-ink-dim" role="status">{{ $t('common.loading') }}</p>
    <div v-else-if="state.phase === 'failed'" class="flex flex-wrap items-center gap-2">
      <InlineError>{{ $t('newConversation.projectsFailed', { error: state.error }) }}</InlineError>
      <button type="button" class="rounded text-xs text-accent underline-offset-2 hover:underline" @click="$emit('retry')">{{ $t('common.tryAgain') }}</button>
    </div>
    <p v-else-if="state.projects.length === 0" class="text-xs text-ink-dim">
      {{ $t('newConversation.noProjects') }}
      <RouterLink :to="{ path: '/settings', hash: '#projects' }" class="text-accent underline-offset-2 hover:underline">{{ $t('newConversation.openSettings') }}</RouterLink>
    </p>
    <template v-else>
      <select
        id="new-conversation-project"
        v-model="model"
        :disabled="disabled"
        aria-describedby="new-conversation-project-help"
        class="h-9 w-full rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent disabled:opacity-60 min-[560px]:w-72"
        data-testid="project-picker"
      >
        <option :value="null">{{ $t('newConversation.projectNone') }}</option>
        <option v-for="project in state.projects" :key="project.id" :value="project.id">{{ project.name }}</option>
      </select>
      <p id="new-conversation-project-help" class="text-xs text-ink-dim">{{ $t('newConversation.projectHelp') }}</p>
    </template>
  </div>
</template>
