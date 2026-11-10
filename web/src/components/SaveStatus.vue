<script setup lang="ts">
import InlineError from './InlineError.vue';

/** What happened to the last change of a settings section. Its room is kept, so the rows below never jump. */
defineProps<{ saved: boolean; error: string | null }>();
</script>

<template>
  <div class="flex min-h-5 items-center py-3" aria-live="polite">
    <InlineError v-if="error" data-testid="settings-error">{{ error }}</InlineError>
    <p v-else-if="saved" role="status" class="text-xs text-ok">{{ $t('settings.saved') }}</p>
  </div>
</template>
