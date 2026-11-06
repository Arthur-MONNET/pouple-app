import { computed, reactive, ref, watch, type Ref } from 'vue';
import { ApiError, errorMessage } from '../api/client';
import type { ConnectorStatus, ValidationFailure } from '../api/types';
import { checklistFor, flowReducer, hasForm, initialFlow, type FlowEvent, type FlowState } from '../lib/connectorFlow';
import { checkField } from '../lib/pattern';
import { useApi } from './useApi';

const asFailure = (cause: unknown): ValidationFailure =>
  cause instanceof ApiError ? { message: cause.message, ...(cause.field && { field: cause.field }) } : { message: errorMessage(cause) };

/**
 * Drives the setup of one connector: validate, show who it is, save once confirmed. The form is generated from the
 * descriptor; a connector with no field (Codex) is checked on its own as soon as the step opens.
 */
export function useConnectorStep(connector: Ref<ConnectorStatus>, onSaved: () => void) {
  const api = useApi();
  const flow = ref<FlowState>(initialFlow);
  /** The values being typed. Secrets live here only until they are saved or dropped. */
  const values = reactive<Record<string, string>>({});
  const touched = reactive<Record<string, boolean>>({});

  const dispatch = (event: FlowEvent): void => void (flow.value = flowReducer(flow.value, event));

  const fields = computed(() => connector.value.descriptor.fields ?? []);
  const withForm = computed(() => hasForm(connector.value));
  const failure = computed(() => (flow.value.phase === 'form' ? flow.value.error : null));
  /** Message under each field: the early check once touched, or what the core said. */
  const fieldErrors = computed<Record<string, string | null>>(() =>
    Object.fromEntries(
      fields.value.map((field) => {
        const early = touched[field.id] ? checkField(field, values[field.id] ?? '') : null;
        const server = failure.value?.field === field.id ? failure.value.message : null;
        return [field.id, early ?? server];
      }),
    ),
  );
  /** A problem that belongs to no field (the Codex check says what is missing this way). */
  const generalError = computed(() => (failure.value && !failure.value.field ? failure.value.message : null));
  const checklist = computed(() => checklistFor(connector.value.descriptor.id, connector.value.descriptor.actions ?? [], failure.value));

  function clearValues(): void {
    for (const key of Object.keys(values)) values[key] = '';
    for (const key of Object.keys(touched)) touched[key] = false;
  }

  /** Asks the core who the values belong to. */
  async function check(): Promise<void> {
    for (const field of fields.value) touched[field.id] = true;
    if (fields.value.some((field) => checkField(field, values[field.id] ?? '') !== null)) return;
    dispatch({ type: 'submit' });
    try {
      const profile = await api.validateConnector(connector.value.descriptor.id, { ...values });
      dispatch({ type: 'validated', profile });
    } catch (cause) {
      dispatch({ type: 'failed', error: asFailure(cause) });
    }
  }

  /** Back to the form without saving anything; what was typed is dropped. */
  function reject(): void {
    clearValues();
    dispatch({ type: 'reject' });
  }

  async function confirm(): Promise<void> {
    if (flow.value.phase !== 'confirm') return;
    dispatch({ type: 'confirm' });
    try {
      await api.saveConnector(connector.value.descriptor.id, { ...values });
      clearValues();
      dispatch({ type: 'saved' });
      onSaved();
    } catch (cause) {
      dispatch({ type: 'failed', error: asFailure(cause) });
    }
  }

  // A fresh connector means a fresh step. A step with no field has nothing to wait for: check it right away.
  watch(
    () => connector.value.descriptor.id,
    () => {
      clearValues();
      flow.value = initialFlow;
      if (!withForm.value) void check();
    },
    { immediate: true },
  );

  return { flow, values, touched, fields, withForm, fieldErrors, generalError, checklist, check, reject, confirm };
}
