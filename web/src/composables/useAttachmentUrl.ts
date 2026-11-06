import { onBeforeUnmount, ref, watch, type Ref } from 'vue';
import { useApi } from './useApi';

/** Loads an attachment with the bearer token and exposes it as an object URL, released when no longer needed. */
export function useAttachmentUrl(requestId: Ref<string>, attachmentId: Ref<string>) {
  const api = useApi();
  const url = ref<string | null>(null);
  const failed = ref(false);

  const release = (): void => {
    if (url.value) URL.revokeObjectURL(url.value);
    url.value = null;
  };

  watch(
    [requestId, attachmentId],
    async () => {
      release();
      failed.value = false;
      try {
        const blob = await api.getAttachment(requestId.value, attachmentId.value);
        url.value = URL.createObjectURL(blob);
      } catch {
        failed.value = true;
      }
    },
    { immediate: true },
  );
  onBeforeUnmount(release);

  return { url, failed };
}
