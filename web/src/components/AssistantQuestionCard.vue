<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { AssistantQuestion, QuestionAnswer } from '../api/types';
import { emptyDraft, isAnswered, OTHER, toAnswer, togglePick, type QuestionDraft } from '../lib/questionAnswers';
import AppButton from './AppButton.vue';

const props = withDefaults(
  defineProps<{
    /** Chip above the card, e.g. "Request from Morgan in #front". */
    title: string;
    questions: AssistantQuestion[];
    busy?: boolean;
    submitLabel?: string;
    /** Offer a free-text answer next to the proposed options. */
    allowOther?: boolean;
    /** Picking an option answers at once; the button is then only for the typed answer. Off for confirmations. */
    submitOnPick?: boolean;
    /** Offer to ignore the whole request in one click. */
    ignorable?: boolean;
  }>(),
  { busy: false, submitLabel: undefined, allowOther: true, submitOnPick: true, ignorable: false },
);
const emit = defineEmits<{ submit: [answers: QuestionAnswer[]]; ignore: [] }>();

const step = ref(0);
const drafts = ref<Record<string, QuestionDraft>>({});

watch(
  () => props.questions.map((question) => question.id).join(),
  () => {
    step.value = 0;
    drafts.value = {};
  },
);

const question = computed(() => props.questions[step.value] as AssistantQuestion);
const draft = computed<QuestionDraft>(() => drafts.value[question.value.id] ?? emptyDraft());
const isLast = computed(() => step.value === props.questions.length - 1);
const letter = (index: number): string => String.fromCharCode(65 + index);

function setDraft(next: QuestionDraft): void {
  drafts.value = { ...drafts.value, [question.value.id]: next };
}

const canContinue = computed(() => isAnswered(question.value, draft.value));
/** A single choice sends on pick; the button is for the typed answer, for confirmations, and always for several options. */
const showAction = computed(() => !props.submitOnPick || question.value.multiple === true || draft.value.choice === OTHER);

const isPicked = (optionId: string): boolean =>
  question.value.multiple ? draft.value.picks.includes(optionId) : draft.value.choice === optionId;

function pick(optionId: string): void {
  if (props.busy) return;
  if (question.value.multiple) {
    setDraft(togglePick(question.value, draft.value, optionId));
    return;
  }
  setDraft({ ...draft.value, choice: optionId });
  if (props.submitOnPick) next();
}

function type(text: string): void {
  // On a single choice, typing picks the typed answer; on several options, it comes in addition to the picks.
  setDraft(question.value.multiple ? { ...draft.value, other: text } : { ...draft.value, other: text, choice: OTHER });
}

function focusOther(): void {
  if (!question.value.multiple) setDraft({ ...draft.value, choice: OTHER });
}

const otherSelected = computed(() => (question.value.multiple ? draft.value.other.trim() !== '' : draft.value.choice === OTHER));

/** Enter sends from the options and the typed answer; on a button (Back, Ignore), it presses that button instead. */
function onEnter(event: KeyboardEvent): void {
  if (event.target instanceof HTMLElement && event.target.closest('button')) return;
  event.preventDefault();
  next();
}

function next(): void {
  if (!canContinue.value) return;
  if (!isLast.value) {
    step.value += 1;
    return;
  }
  emit(
    'submit',
    props.questions.map((item) => toAnswer(item, drafts.value[item.id] ?? emptyDraft())),
  );
}
</script>

<template>
  <section class="flex flex-col gap-2" data-testid="assistant-question" @keydown.enter.exact="onEnter">
    <span class="w-fit max-w-full truncate rounded-lg bg-raised px-3 py-1.5 text-xs text-ink">{{ title }}</span>
    <div class="rounded-xl border border-line bg-surface p-3">
      <p class="font-medium text-ink" :class="{ 'mb-1': question.multiple, 'mb-3': !question.multiple }">{{ question.text }}</p>
      <p v-if="question.multiple" class="mb-3 text-xs text-ink-muted">{{ $t('question.severalHint') }}</p>
      <ul class="flex flex-col gap-1.5" :role="question.multiple ? 'group' : 'radiogroup'" :aria-label="question.text">
        <li v-for="(option, index) in question.options" :key="option.id">
          <button
            type="button"
            :role="question.multiple ? 'checkbox' : 'radio'"
            :aria-checked="isPicked(option.id)"
            class="flex w-full items-start gap-2.5 rounded-lg border px-2.5 py-2 text-left transition motion-reduce:transition-none"
            :class="{
              'border-accent bg-selected text-ink': isPicked(option.id),
              'border-line text-ink-muted hover:bg-raised hover:text-ink': !isPicked(option.id),
            }"
            @click="pick(option.id)"
          >
            <span class="mt-px flex size-5 shrink-0 items-center justify-center rounded border border-line-strong font-mono text-2xs">{{ letter(index) }}</span>
            <span class="min-w-0">
              <span class="block">{{ option.label }}</span>
              <span v-if="option.description" class="block text-xs text-ink-muted">{{ option.description }}</span>
            </span>
          </button>
        </li>
        <li v-if="allowOther">
          <div class="flex items-center gap-2.5 rounded-lg border px-2.5 py-2" :class="{ 'border-accent bg-selected': otherSelected, 'border-line': !otherSelected }">
            <span class="flex size-5 shrink-0 items-center justify-center rounded border border-line-strong font-mono text-2xs">{{ letter(question.options.length) }}</span>
            <input
              :value="draft.other"
              type="text"
              maxlength="1000"
              :aria-label="$t('question.otherAria', { question: question.text })"
              :placeholder="$t('question.other')"
              class="min-w-0 flex-1 bg-transparent text-ink outline-none"
              @input="type(($event.target as HTMLInputElement).value)"
              @focus="focusOther"
            />
          </div>
        </li>
      </ul>
      <div class="mt-3 flex items-center gap-3">
        <div class="flex gap-1" role="img" :aria-label="$t('question.progress', { n: step + 1, total: questions.length })">
          <span v-for="(item, index) in questions" :key="item.id" class="size-1.5 rounded-full" :class="{ 'bg-ink': index === step, 'bg-ink-faint': index !== step }" />
        </div>
        <AppButton v-if="ignorable" variant="ghost" size="sm" :disabled="busy" data-testid="ignore-request" @click="emit('ignore')">{{ $t('question.ignore') }}</AppButton>
        <div class="ml-auto flex gap-1.5">
          <AppButton v-if="step > 0" variant="ghost" size="sm" @click="step -= 1">{{ $t('question.back') }}</AppButton>
          <AppButton v-if="showAction" variant="primary" size="sm" :disabled="!canContinue || busy" @click="next">
            {{ isLast ? (submitLabel ?? $t('question.send')) : $t('question.next') }}
          </AppButton>
        </div>
      </div>
    </div>
  </section>
</template>
