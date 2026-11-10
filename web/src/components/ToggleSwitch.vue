<script setup lang="ts">
import { useId } from 'vue';

// The data-testid and other attributes belong to the switch itself, not to the row around it.
defineOptions({ inheritAttrs: false });

defineProps<{ label: string; description?: string; disabled?: boolean }>();
const model = defineModel<boolean>({ required: true });

const labelId = useId();
const descriptionId = useId();
</script>

<template>
  <div class="flex items-start justify-between gap-4">
    <div class="min-w-0">
      <span :id="labelId" class="text-sm font-medium text-ink">{{ label }}</span>
      <span v-if="description" :id="descriptionId" class="mt-0.5 block text-xs leading-relaxed text-ink-muted">{{ description }}</span>
    </div>
    <button
      v-bind="$attrs"
      type="button"
      role="switch"
      :aria-checked="model"
      :aria-labelledby="labelId"
      :aria-describedby="description ? descriptionId : undefined"
      :disabled="disabled"
      class="relative mt-0.5 h-6 w-11 shrink-0 rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none"
      :class="{ 'bg-accent': model, 'bg-line-strong': !model }"
      @click="model = !model"
    >
      <span
        class="absolute left-0.5 top-0.5 size-5 rounded-full bg-white shadow transition-transform motion-reduce:transition-none"
        :class="{ 'translate-x-5': model }"
        aria-hidden="true"
      />
    </button>
  </div>
</template>
