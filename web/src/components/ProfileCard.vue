<script setup lang="ts">
import type { Profile } from '../api/types';
import AppIcon from './AppIcon.vue';
import ProfileAvatar from './ProfileAvatar.vue';

defineProps<{ profile: Profile }>();
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center gap-4">
      <ProfileAvatar :name="profile.name" :url="profile.avatarUrl" size="lg" />
      <div class="min-w-0">
        <p class="truncate text-lg font-semibold text-ink" data-testid="profile-name">{{ profile.name }}</p>
        <p v-if="profile.subtitle" class="truncate text-sm text-ink-muted">{{ profile.subtitle }}</p>
      </div>
    </div>

    <dl v-if="(profile.facts ?? []).length > 0" class="divide-y divide-line-soft rounded-lg border border-line bg-surface">
      <div v-for="fact in profile.facts ?? []" :key="fact.label" class="flex items-baseline justify-between gap-4 px-3 py-2">
        <dt class="text-xs text-ink-dim">{{ fact.label }}</dt>
        <dd class="min-w-0 truncate text-right text-sm text-ink">{{ fact.value }}</dd>
      </div>
    </dl>

    <ul v-if="(profile.warnings ?? []).length > 0" class="flex flex-col gap-2" :aria-label="$t('connect.warnings')">
      <li v-for="warning in profile.warnings ?? []" :key="warning.title" class="flex gap-2.5 rounded-lg border border-warn/30 bg-warn/10 px-3 py-2.5">
        <span class="mt-0.5 text-warn"><AppIcon name="alert" /></span>
        <div class="min-w-0">
          <p class="text-sm font-medium text-warn">{{ warning.title }}</p>
          <p class="text-xs text-ink-muted">{{ warning.detail }}</p>
        </div>
      </li>
    </ul>
  </div>
</template>
