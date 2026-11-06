import { onBeforeUnmount, ref, watch, type Ref } from 'vue';
import { errorMessage } from '../api/client';
import { t } from '../i18n';
import type { AttachmentRef } from '../api/types';
import { useApi } from './useApi';

export const MAX_BYTES = 8 * 1024 * 1024;
export const MAX_FILES = 6;
const ACCEPTED = /^(image\/(png|jpeg|gif|webp)|text\/.+|application\/(json|xml))$/;

export interface DraftFile {
  ref: AttachmentRef;
  /** A local preview of an image, so it shows at once. */
  preview: string | null;
}

/** Why a file cannot be added, or null when it can. */
export function fileProblem(file: Pick<File, 'name' | 'size' | 'type'>, alreadyAdded: number): string | null {
  const name = file.name || t('files.thisFile');
  if (alreadyAdded >= MAX_FILES) return t('files.tooMany', { max: MAX_FILES });
  if (file.size > MAX_BYTES) return t('files.tooLarge', { name });
  if (!ACCEPTED.test(file.type)) return t('files.unsupported', { name });
  return null;
}

/**
 * The files being added to the next message of a request: each is uploaded as soon as it is chosen, pasted or dropped,
 * so sending the message only has to name them.
 */
export function useComposerFiles(requestId: Ref<string | null>) {
  const api = useApi();
  const files = ref<DraftFile[]>([]);
  const uploading = ref(false);
  const error = ref<string | null>(null);

  function forget(): void {
    for (const file of files.value) if (file.preview) URL.revokeObjectURL(file.preview);
    files.value = [];
    error.value = null;
  }

  async function add(list: Iterable<File>): Promise<void> {
    const id = requestId.value;
    if (!id) return;
    error.value = null;
    uploading.value = true;
    try {
      for (const file of list) {
        const problem = fileProblem(file, files.value.length);
        if (problem) throw new Error(problem);
        const uploaded = await api.uploadAttachment(id, file);
        files.value = [...files.value, { ref: uploaded, preview: uploaded.kind === 'image' ? URL.createObjectURL(file) : null }];
      }
    } catch (cause) {
      error.value = errorMessage(cause);
    } finally {
      uploading.value = false;
    }
  }

  const onPaste = (event: ClipboardEvent): void => {
    const pasted = [...(event.clipboardData?.files ?? [])];
    // A pasted screenshot is a file; pasted text stays text.
    if (pasted.length > 0) {
      event.preventDefault();
      void add(pasted);
    }
  };

  const onDrop = (event: DragEvent): void => void add([...(event.dataTransfer?.files ?? [])]);

  function remove(id: string): void {
    const gone = files.value.find((file) => file.ref.id === id);
    if (gone?.preview) URL.revokeObjectURL(gone.preview);
    files.value = files.value.filter((file) => file.ref.id !== id);
  }

  // Files belong to the request they were added to: another target means a fresh start.
  watch(requestId, forget);
  onBeforeUnmount(forget);

  return { files, uploading, error, add, remove, forget, onPaste, onDrop };
}
