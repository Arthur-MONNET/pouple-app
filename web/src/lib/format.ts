/** "now", "5 min ago", "3 h ago", "yesterday", then "Nov 4". */
export function relativeTime(iso: string, now: Date = new Date()): string {
  const date = new Date(iso);
  const seconds = Math.round((now.getTime() - date.getTime()) / 1000);
  if (Number.isNaN(seconds)) return '';
  if (seconds < 45) return 'now';
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  const days = Math.round(hours / 24);
  if (days === 1) return 'yesterday';
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

/** "14:02" for today, "Nov 4, 14:02" otherwise. */
export function formatReceived(iso: string, now: Date = new Date()): string {
  const date = new Date(iso);
  const time = date.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
  return date.toDateString() === now.toDateString()
    ? time
    : `${date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}, ${time}`;
}

/** Up to two initials, for an avatar without a picture. */
export function initials(name: string): string {
  const letters = name
    .split(/[\s@._-]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0]?.toUpperCase() ?? '');
  return letters.join('') || '?';
}

export const sizeLabel = (bytes: number): string =>
  bytes < 1024 * 1024 ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`;
