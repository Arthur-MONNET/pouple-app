<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { AssistantQuestion, QuestionAnswer } from '../api/types';
import AppButton from './AppButton.vue';

const OTHER = '__other';

const props = withDefaults(
  defineProps<{
    /** Chip above the card, e.g. "Request from Morgan in #front". */
    title: string;
    questions: AssistantQuestion[];
    busy?: boolean;
    submitLabel?: string;
    /** Offer a free-text answer next to the proposed options. */
    allowOther?: boolean;
  }>(),
  { busy: false, submitLabel: undefined, allowOther: true },
);
const emit = defineEmits<{ submit: [answers: QuestionAnswer[]] }>();

const step = ref(0);
const choices = ref<Record<string, string>>({});
const others = ref<Record<string, string>>({});

watch(
  () => props.questions.map((question) => question.id).join(),
  () => {
    step.value = 0;
    choices.value = {};
    others.value = {};
  },
);

const question = computed(() => props.questions[step.value] as AssistantQuestion);
const isLast = computed(() => step.value === props.questions.length - 1);
const letter = (index: number): string => String.fromCharCode(65 + index);

function isAnswered(item: AssistantQuestion): boolean {
  const choice = choices.value[item.id];
  return choice === OTHER ? (others.value[item.id] ?? '').trim() !== '' : choice !== undefined;
}

const canContinue = computed(() => isAnswered(question.value));

function next(): void {
  if (!canContinue.value) return;
  if (!isLast.value) {
    step.value += 1;
    return;
  }
  emit(
    'submit',
    props.questions.map((item): QuestionAnswer => {
      const choice = choices.value[item.id];
      return choice === OTHER ? { questionId: item.id, text: (others.value[item.id] ?? '').trim() } : { questionId: item.id, optionId: choice as string };
    }),
  );
}
</script>

<template>
  <section class="flex flex-col gap-2" data-testid="assistant-question" @keydown.enter.exact.prevent="next">
    <span class="w-fit max-w-full truncate rounded-lg bg-raised px-3 py-1.5 text-xs text-ink">{{ title }}</span>
    <div class="rounded-xl border border-line bg-surface p-3">
      <p class="mb-3 font-medium text-ink">{{ question.text }}</p>
      <ul class="flex flex-col gap-1.5" role="radiogroup" :aria-label="question.text">
        <li v-for="(option, index) in question.options" :key="option.id">
          <button
            type="button"
            role="radio"
            :aria-checked="choices[question.id] === option.id"
            class="flex w-full items-start gap-2.5 rounded-lg border px-2.5 py-2 text-left transition motion-reduce:transition-none"
            :class="choices[question.id] === option.id ? 'border-accent bg-selected text-ink' : 'border-line text-ink-muted hover:bg-raised hover:text-ink'"
            @click="choices[question.id] = option.id"
          >
            <span class="mt-px flex size-5 shrink-0 items-center justify-center rounded border border-line-strong font-mono text-2xs">{{ letter(index) }}</span>
            <span class="min-w-0">
              <span class="block">{{ option.label }}</span>
              <span v-if="option.description" class="block text-xs text-ink-muted">{{ option.description }}</span>
            </span>
          </button>
        </li>
        <li v-if="allowOther">
          <div
            class="flex items-center gap-2.5 rounded-lg border px-2.5 py-2"
            :class="choices[question.id] === OTHER ? 'border-accent bg-selected' : 'border-line'"
          >
            <span class="flex size-5 shrink-0 items-center justify-center rounded border border-line-strong font-mono text-2xs">{{ letter(question.options.length) }}</span>
            <input
              v-model="others[question.id]"
              type="text"
              maxlength="1000"
              :aria-label="$t('question.otherAria', { question: question.text })"
              :placeholder="$t('question.other')"
              class="min-w-0 flex-1 bg-transparent text-ink outline-none"
              @focus="choices[question.id] = OTHER"
            />
          </div>
        </li>
      </ul>
      <div class="mt-3 flex items-center gap-3">
        <div class="flex gap-1" role="img" :aria-label="$t('question.progress', { n: step + 1, total: questions.length })">
          <span
            v-for="(item, index) in questions"
            :key="item.id"
            class="size-1.5 rounded-full"
            :class="index === step ? 'bg-ink' : 'bg-ink-faint'"
          />
        </div>
        <div class="ml-auto flex gap-1.5">
          <AppButton v-if="step > 0" variant="ghost" size="sm" @click="step -= 1">{{ $t('question.back') }}</AppButton>
          <AppButton variant="primary" size="sm" :disabled="!canContinue || busy" @click="next">
            {{ isLast ? (submitLabel ?? $t('question.send')) : $t('question.next') }}
          </AppButton>
        </div>
      </div>
    </div>
  </section>
</template>
