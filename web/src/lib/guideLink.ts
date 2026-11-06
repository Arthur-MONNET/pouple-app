import type { Link } from '../api/types';

/** A link to a self-hosted service opens on the address the user typed, when it is a plain https address. */
export function linkTarget(link: Link, values?: Record<string, string>): string {
  const typed = link.hostField ? values?.[link.hostField]?.trim() : '';
  if (!typed) return link.url;
  try {
    const host = new URL(typed);
    if (host.protocol !== 'https:') return link.url;
    const url = new URL(link.url);
    url.protocol = host.protocol;
    url.host = host.host;
    return url.toString();
  } catch {
    return link.url;
  }
}
