import { describe, expect, it } from 'vitest';
import { checkField } from './pattern';

const slack = { label: 'User OAuth Token', pattern: '^xox[a-z]-[A-Za-z0-9-]{20,}$', patternHint: 'does not look like a Slack token (it starts with xoxp-)' };

describe('checkField', () => {
  it('asks for a value', () => {
    expect(checkField(slack, '   ')).toBe('User OAuth Token is required');
  });

  it('refuses what does not match, with the hint of the connector', () => {
    expect(checkField(slack, 'hello')).toBe('User OAuth Token does not look like a Slack token (it starts with xoxp-)');
    expect(checkField(slack, 'xoxp-short')).not.toBeNull();
  });

  it('accepts a well-formed token, ignoring surrounding spaces', () => {
    expect(checkField(slack, ' xoxp-1234567890-abcdefghijklmnop ')).toBeNull();
  });

  it('falls back to a generic hint', () => {
    expect(checkField({ label: 'Key', pattern: '^a$' }, 'b')).toBe('Key does not look right');
  });

  it('lets the core judge a pattern this engine cannot read', () => {
    expect(checkField({ label: 'Key', pattern: '(?<' }, 'anything')).toBeNull();
  });

  it('only requires a value when there is no pattern', () => {
    expect(checkField({ label: 'Name' }, 'x')).toBeNull();
  });
});
