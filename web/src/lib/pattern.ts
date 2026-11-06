import type { Field } from '../api/types';

/**
 * The early check of a form field, with the wording of the core's own check so both say the same thing. Returns the
 * message to show, or null when the value looks right. The core stays the authority: this only saves a round trip.
 */
export function checkField(field: Pick<Field, 'label' | 'pattern' | 'patternHint'>, rawValue: string): string | null {
  const value = rawValue.trim();
  if (value === '') return `${field.label} is required`;
  if (!field.pattern) return null;
  let matches = true;
  try {
    matches = new RegExp(field.pattern).test(value);
  } catch {
    // A pattern this engine cannot read is left to the core.
    return null;
  }
  return matches ? null : `${field.label} ${field.patternHint || 'does not look right'}`;
}
