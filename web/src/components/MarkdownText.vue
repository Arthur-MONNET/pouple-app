<script setup lang="ts">
import { computed } from 'vue';
import { parseMarkdown, type Inline } from '../lib/markdown';

const props = defineProps<{ source: string }>();
const blocks = computed(() => parseMarkdown(props.source));
</script>

<template>
  <div class="flex flex-col gap-2 break-words [&_a]:text-accent [&_a:hover]:underline">
    <template v-for="(block, index) in blocks" :key="index">
      <p v-if="block.type === 'paragraph'" class="whitespace-pre-line">
        <template v-for="(part, i) in block.inlines" :key="i"><MarkdownInline :part="part" /></template>
      </p>
      <p v-else-if="block.type === 'heading'" class="font-semibold">
        <template v-for="(part, i) in block.inlines" :key="i"><MarkdownInline :part="part" /></template>
      </p>
      <component :is="block.ordered ? 'ol' : 'ul'" v-else-if="block.type === 'list'" class="flex flex-col gap-1 pl-5" :class="block.ordered ? 'list-decimal' : 'list-disc'">
        <li v-for="(item, i) in block.items" :key="i">
          <template v-for="(part, j) in item" :key="j"><MarkdownInline :part="part" /></template>
        </li>
      </component>
      <pre v-else class="overflow-x-auto rounded-md bg-canvas px-2.5 py-2 font-mono text-xs">{{ block.text }}</pre>
    </template>
  </div>
</template>

<script lang="ts">
import { defineComponent, h, type PropType } from 'vue';

/** One inline fragment. Rendered with text children only, so nothing from the model can become markup. */
const MarkdownInline = defineComponent({
  props: { part: { type: Object as PropType<Inline>, required: true } },
  setup(props) {
    return () => {
      const { part } = props;
      switch (part.type) {
        case 'bold':
          return h('strong', { class: 'font-semibold' }, part.text);
        case 'italic':
          return h('em', part.text);
        case 'code':
          return h('code', { class: 'rounded bg-canvas px-1 py-0.5 font-mono text-xs' }, part.text);
        case 'link':
          return h('a', { href: part.href, target: '_blank', rel: 'noopener noreferrer' }, part.text);
        default:
          return part.text;
      }
    };
  },
});
export default { components: { MarkdownInline } };
</script>
