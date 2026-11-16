import type { AssistantAlert } from '../api/types';
import { lang, t, type Lang } from '../i18n';

const BCP47: Record<Lang, string> = { fr: 'fr-FR', en: 'en-GB' };

const pad = (value: number): string => String(value).padStart(2, '0');

/** The local date, "2026-11-20": what names a day, whatever the language. */
const dayKey = (date: Date): string => `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;

/** The value of an `<input type="datetime-local">` for an instant: its local date and time, to the minute. */
export function toLocalInput(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? '' : `${dayKey(date)}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

const LOCAL_INPUT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/;

/** Whether a date and time typed in the interface is complete and in the future. A date and time without zone is read on this clock. */
export function isUpcoming(local: string, now: Date = new Date()): boolean {
  return LOCAL_INPUT.test(local) && new Date(local).getTime() > now.getTime();
}

/** The next whole hour, at least `minutes` ahead: the time a new alert starts from. */
export function nextHour(now: Date = new Date(), minutes = 30): string {
  const start = new Date(now.getTime() + minutes * 60_000);
  start.setMinutes(0, 0, 0);
  start.setHours(start.getHours() + 1);
  return toLocalInput(start.toISOString());
}

/** "09:00" in French, "09:00" in English: the time of day. */
export function formatClock(iso: string, locale: Lang = lang.value): string {
  return new Date(iso).toLocaleTimeString(BCP47[locale], { hour: '2-digit', minute: '2-digit' });
}

/** "ven. 20 nov., 09:00": a suggested moment, short enough for a list. */
export function formatMoment(iso: string, locale: Lang = lang.value): string {
  const date = new Date(iso);
  const day = date.toLocaleDateString(BCP47[locale], { weekday: 'short', day: 'numeric', month: 'short' });
  return `${day}, ${formatClock(iso, locale)}`;
}

export interface AlertDay {
  /** The local day, "2026-11-20": stable across refreshes. */
  key: string;
  /** "Aujourd'hui", "Demain", then "vendredi 20 novembre". */
  label: string;
  alerts: AssistantAlert[];
}

/** Groups alerts under the day they fire, soonest day first. Alerts keep the order they came in. */
export function groupByDay(alerts: readonly AssistantAlert[], now: Date = new Date(), locale: Lang = lang.value): AlertDay[] {
  const tomorrow = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
  const days = new Map<string, AlertDay>();
  for (const alert of [...alerts].sort((a, b) => new Date(a.at).getTime() - new Date(b.at).getTime())) {
    const at = new Date(alert.at);
    const key = dayKey(at);
    let day = days.get(key);
    if (!day) {
      const label =
        key === dayKey(now)
          ? t('alerts.today')
          : key === dayKey(tomorrow)
            ? t('alerts.tomorrow')
            : at.toLocaleDateString(BCP47[locale], { weekday: 'long', day: 'numeric', month: 'long' });
      day = { key, label, alerts: [] };
      days.set(key, day);
    }
    day.alerts.push(alert);
  }
  return [...days.values()].sort((a, b) => a.key.localeCompare(b.key));
}
