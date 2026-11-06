<script setup lang="ts">
import type { AttachmentRef } from '../api/types';
import { useAttachmentUrl } from '../composables/useAttachmentUrl';
import { sizeLabel } from '../lib/format';

const props = defineProps<{ requestId: string; attachments: AttachmentRef[] }>();
</script>

<template>
  <ul class="flex flex-wrap gap-2" aria-label="Attached files">
    <li v-for="file in attachments" :key="file.id">
      <AttachmentItem :request-id="props.requestId" :file="file" />
    </li>
  </ul>
</template>

<script lang="ts">
import { computed, defineComponent, h, type PropType } from 'vue';

/** One attachment. The bytes come with the bearer token, so they are fetched and shown from an object URL. */
const AttachmentItem = defineComponent({
  props: { requestId: { type: String, required: true }, file: { type: Object as PropType<AttachmentRef>, required: true } },
  setup(props) {
    const { url, failed } = useAttachmentUrl(
      computed(() => props.requestId),
      computed(() => props.file.id),
    );
    return () => {
      const { file } = props;
      if (file.kind === 'image') {
        return url.value
          ? h('a', { href: url.value, target: '_blank', rel: 'noopener noreferrer', class: 'block overflow-hidden rounded-lg border border-line', title: file.name }, [
              h('img', { src: url.value, alt: file.name, class: 'max-h-36 max-w-full object-contain' }),
            ])
          : h('span', { class: 'flex h-20 w-28 items-center justify-center rounded-lg border border-line text-2xs text-ink-dim' }, failed.value ? 'Unavailable' : 'Loading…');
      }
      return h(
        'a',
        { href: url.value ?? undefined, download: file.name, class: 'flex items-center gap-1.5 rounded-md border border-line px-2 py-1 text-xs text-ink-muted hover:bg-raised hover:text-ink' },
        [h('span', { class: 'max-w-40 truncate' }, file.name), h('span', { class: 'text-2xs text-ink-dim' }, sizeLabel(file.size))],
      );
    };
  },
});
export default { components: { AttachmentItem } };
</script>
