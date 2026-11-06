<script setup lang="ts">
import ProjectsFolderForm from './ProjectsFolderForm.vue';

defineEmits<{ next: [] }>();
</script>

<template>
  <div class="mx-auto flex max-w-xl flex-col gap-6 py-6 min-[700px]:py-10">
    <header class="flex flex-col gap-1">
      <p class="text-xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('projects.optional') }}</p>
      <h1 class="font-display text-3xl font-semibold text-ink">{{ $t('projects.title') }}</h1>
      <p class="text-sm text-ink-muted">{{ $t('projects.later') }}</p>
    </header>
    <div class="rounded-2xl border border-line bg-surface/60 p-5 min-[700px]:p-6">
      <ProjectsFolderForm skippable :save-label="$t('projects.saveContinue')" @saved="$emit('next')" @skip="$emit('next')" />
    </div>
  </div>
</template>
