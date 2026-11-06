import { lang, t, type Lang } from '../i18n';

const BCP47: Record<Lang, string> = { fr: 'fr-FR', en: 'en-GB' };
const tag = (locale: Lang): string => BCP47[locale];

/** "à l'instant", "il y a 5 min", "hier", then "4 nov." : through Intl, in the active language. */
export function relativeTime(iso: string, now: Date = new Date(), locale: Lang = lang.value): string {
  const date = new Date(iso);
  const seconds = Math.round((now.getTime() - date.getTime()) / 1000);
  if (Number.isNaN(seconds)) return '';
  if (seconds < 45) return t('format.now', {}, { locale });
  const format = new Intl.RelativeTimeFormat(tag(locale), { numeric: 'auto', style: 'short' });
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return format.format(-minutes, 'minute');
  const hours = Math.round(minutes / 60);
  if (hours < 24) return format.format(-hours, 'hour');
  const days = Math.round(hours / 24);
  if (days === 1) return format.format(-1, 'day');
  return date.toLocaleDateString(tag(locale), { month: 'short', day: 'numeric' });
}

/** "14:02" for today, "4 nov., 14:02" otherwise. */
export function formatReceived(iso: string, now: Date = new Date(), locale: Lang = lang.value): string {
  const date = new Date(iso);
  const time = date.toLocaleTimeString(tag(locale), { hour: '2-digit', minute: '2-digit' });
  return date.toDateString() === now.toDateString()
    ? time
    : `${date.toLocaleDateString(tag(locale), { month: 'short', day: 'numeric' })}, ${time}`;
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

export function sizeLabel(bytes: number): string {
  if (bytes < 1024 * 1024) return t('files.sizeKb', { n: Math.max(1, Math.round(bytes / 1024)) });
  return t('files.sizeMb', { n: (bytes / 1024 / 1024).toLocaleString(tag(lang.value), { maximumFractionDigits: 1, minimumFractionDigits: 1 }) });
}

/** "28 oct. 2026" : a day, with its year, in the active language. */
export function formatDay(iso: string, locale: Lang = lang.value): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString(tag(locale), { day: 'numeric', month: 'short', year: 'numeric' });
}
