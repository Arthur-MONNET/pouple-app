import { computed, onMounted, ref } from 'vue';
import { errorMessage } from '../api/client';
import type { ProjectInfo } from '../api/types';
import { t } from '../i18n';
import { conversationPayload, draftProblem, MAX_MESSAGE_BYTES, type ConversationDraft } from '../lib/newConversation';
import { useAssistantStore } from './assistantStore';
import { useComposerFiles } from './composerFiles';
import { useApi } from './useApi';

export type ProjectsState = { phase: 'loading' } | { phase: 'ready'; projects: ProjectInfo[] } | { phase: 'failed'; error: string };

/**
 * The composer of a new conversation: the text, the files (uploaded as they are added), the optional project, and the
 * submission. The core validates again and may refuse (400, 409): its message is shown as it is.
 */
export function useNewConversation() {
  const api = useApi();
  const store = useAssistantStore();

  const text = ref('');
  const projectId = ref<string | null>(null);
  const submitting = ref(false);
  const error = ref<string | null>(null);
  const projects = ref<ProjectsState>({ phase: 'loading' });
  // Draft files are not tied to a request yet: one fixed target keeps them for the whole draft.
  const files = useComposerFiles(ref<string | null>('draft'), (_target, file) => api.uploadDraftAttachment(file));

  const draft = computed<ConversationDraft>(() => ({ text: text.value, attachmentIds: files.files.value.map((file) => file.ref.id), projectId: projectId.value }));
  const problem = computed(() => draftProblem(draft.value));
  /** Only a draft that is too long says so; an empty one simply cannot be sent yet. */
  const draftError = computed(() => (problem.value === 'tooLong' ? t('newConversation.tooLong', { max: MAX_MESSAGE_BYTES }) : null));

  const active = computed(() => store.status.value?.active === true);
  /** Why nothing can be started now, once the state is known: the assistant is off, or no AI is connected. */
  const inactiveReason = computed(() => {
    if (!store.loaded.value || active.value) return null;
    const why = store.status.value?.problem;
    return why ? t('newConversation.inactiveWith', { problem: why }) : t('newConversation.inactive');
  });
  const canSubmit = computed(() => active.value && !submitting.value && !files.uploading.value && problem.value === null);

  async function loadProjects(): Promise<void> {
    projects.value = { phase: 'loading' };
    try {
      projects.value = { phase: 'ready', projects: await api.listProjects() };
    } catch (cause) {
      projects.value = { phase: 'failed', error: errorMessage(cause) };
    }
  }

  /** Sends the draft; resolves with the new conversation's id, or null when it was not sent (the error says why). */
  async function submit(): Promise<string | null> {
    if (!canSubmit.value) return null;
    submitting.value = true;
    error.value = null;
    try {
      const id = await store.createConversation(conversationPayload(draft.value));
      text.value = '';
      projectId.value = null;
      files.forget();
      return id;
    } catch (cause) {
      error.value = errorMessage(cause);
      return null;
    } finally {
      submitting.value = false;
    }
  }

  onMounted(() => void loadProjects());

  return { text, projectId, files, projects, submitting, error, draftError, inactiveReason, canSubmit, submit, loadProjects };
}
