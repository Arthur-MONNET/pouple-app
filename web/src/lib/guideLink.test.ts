import { describe, expect, it } from 'vitest';
import { linkTarget } from './guideLink';

const link = { label: 'Open', url: 'https://gitlab.com/-/user_settings/personal_access_tokens?name=Pouple', hostField: 'url' };

describe('linkTarget', () => {
  it('keeps the link when nothing was typed or the link has no host field', () => {
    expect(linkTarget(link)).toBe(link.url);
    expect(linkTarget(link, { url: '  ' })).toBe(link.url);
    expect(linkTarget({ label: 'x', url: 'https://a.example/p' }, { url: 'https://b.example' })).toBe('https://a.example/p');
  });

  it('opens the page of the typed server, keeping path and query', () => {
    expect(linkTarget(link, { url: 'https://git.company.example' })).toBe('https://git.company.example/-/user_settings/personal_access_tokens?name=Pouple');
  });

  it('never follows an address that is not plain https', () => {
    for (const typed of ['http://git.company.example', 'javascript:alert(1)', 'not a url', 'ftp://x']) expect(linkTarget(link, { url: typed })).toBe(link.url);
  });
});
