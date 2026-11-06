import type { Lang } from '../i18n';
import { ApiError } from './client';
import type { ConnectorStatus, ConnectorValues, Field, GuideStep, Profile } from './types';

/**
 * The optional connectors of the mock backend (GitLab, Asana, Slite), in both languages. Like the real core they are
 * ordinary connector statuses with `required: false`. Accepted tokens, for a quick try in the browser:
 *   GitLab  glpat-abcdefghijklmnopqrstu      Asana  1/1234567890:abcdefghijklmnopqrstuvwx      Slite  any 24+ letters or digits
 * Any token containing "bad" is refused with a 422 on the `token` field.
 */
export const EXTRA_IDS = ['gitlab', 'asana', 'slite'] as const;
export type ExtraId = (typeof EXTRA_IDS)[number];
export const isExtraId = (id: string): id is ExtraId => (EXTRA_IDS as readonly string[]).includes(id);

interface ExtraText {
  tagline: string;
  tokenLabel: string;
  tokenHelp: string;
  patternHint: string;
  /** What the core says when the service refuses the token. */
  refused: string;
  linkLabel: string;
  guide: [GuideStep['title'], GuideStep['body']][];
  profile: { subtitle: string; facts: string[]; values: string[]; warning?: { title: string; detail: string } };
  /** GitLab only: the address of a self-hosted instance. */
  host?: { label: string; help: string; hint: string };
}

interface ExtraSpec {
  name: string;
  order: number;
  placeholder: string;
  pattern: string;
  link: string;
  profileName: string;
  text: Record<Lang, ExtraText>;
}

const SPECS: Record<ExtraId, ExtraSpec> = {
  gitlab: {
    name: 'GitLab',
    order: 10,
    placeholder: 'glpat-…',
    pattern: '^glpat-[A-Za-z0-9_-]{20,}$',
    link: 'https://gitlab.com/-/user_settings/personal_access_tokens',
    profileName: 'Arthur Monnet',
    text: {
      fr: {
        tagline: 'Tes merge requests et tes tickets',
        tokenLabel: 'Jeton d’accès personnel',
        tokenHelp: 'Il commence par glpat-. Il reste dans le trousseau de ton Mac et ne s’affiche plus jamais.',
        patternHint: 'ne ressemble pas à un jeton GitLab (il commence par glpat-)',
        refused: 'GitLab refuse ce jeton (401 Unauthorized). Recopie-le depuis tes jetons d’accès personnels.',
        linkLabel: 'Ouvrir les jetons GitLab',
        guide: [
          ['Crée un jeton d’accès personnel', 'Dans GitLab, ouvre Préférences, Jetons d’accès, puis Ajouter un nouveau jeton. Un clic ci-dessous t’y emmène.'],
          ['Choisis la permission read_api', 'Coche uniquement read_api : Pouple ne fait que lire tes merge requests et tes tickets. Mets une date d’expiration.'],
          ['Copie le jeton', 'GitLab ne l’affiche qu’une fois. Colle-le ici. Pour une instance auto-hébergée, renseigne aussi son adresse.'],
        ],
        profile: {
          subtitle: '@arthur.monnet · gitlab.com',
          facts: ['Instance', 'Projets', 'Merge requests ouvertes'],
          values: ['gitlab.com', '12', '3'],
          warning: { title: 'Le jeton expire dans 9 jours', detail: 'Pense à en créer un nouveau avant, puis reconnecte GitLab ici.' },
        },
        host: { label: 'Adresse de ton instance', help: 'Seulement pour une instance auto-hébergée. Laisse vide pour gitlab.com.', hint: 'doit commencer par https://' },
      },
      en: {
        tagline: 'Your merge requests and issues',
        tokenLabel: 'Personal access token',
        tokenHelp: 'Starts with glpat-. It stays in your Mac’s keychain and is never shown again.',
        patternHint: 'does not look like a GitLab token (it starts with glpat-)',
        refused: 'GitLab does not accept this token (401 Unauthorized). Copy it again from your personal access tokens.',
        linkLabel: 'Open GitLab tokens',
        guide: [
          ['Create a personal access token', 'In GitLab, open Preferences, Access tokens, then Add new token. The button below takes you there.'],
          ['Pick the read_api scope', 'Tick read_api only: Pouple just reads your merge requests and issues. Set an expiry date.'],
          ['Copy the token', 'GitLab shows it once. Paste it here. For a self-hosted instance, enter its address too.'],
        ],
        profile: {
          subtitle: '@arthur.monnet · gitlab.com',
          facts: ['Instance', 'Projects', 'Open merge requests'],
          values: ['gitlab.com', '12', '3'],
          warning: { title: 'The token expires in 9 days', detail: 'Create a new one before then, and reconnect GitLab here.' },
        },
        host: { label: 'Your instance address', help: 'Only for a self-hosted instance. Leave empty for gitlab.com.', hint: 'must start with https://' },
      },
    },
  },
  asana: {
    name: 'Asana',
    order: 11,
    placeholder: '1/1234567890:…',
    pattern: '^[0-9]/[0-9]+:[A-Za-z0-9]{20,}$',
    link: 'https://app.asana.com/0/my-apps',
    profileName: 'Arthur Monnet',
    text: {
      fr: {
        tagline: 'Tes tâches et tes projets',
        tokenLabel: 'Jeton d’accès personnel',
        tokenHelp: 'Il ressemble à 1/123…:abc…. Il reste dans le trousseau de ton Mac et ne s’affiche plus jamais.',
        patternHint: 'ne ressemble pas à un jeton Asana (par exemple 1/1234567890:abcdef…)',
        refused: 'Asana refuse ce jeton (401 Not Authorized). Recopie-le depuis ta console de développement.',
        linkLabel: 'Ouvrir la console Asana',
        guide: [
          ['Ouvre ta console de développement', 'Dans Asana, va dans la console des applications, section Jetons d’accès personnels.'],
          ['Crée un jeton', 'Clique sur Créer un nouveau jeton, donne-lui le nom Pouple et accepte les conditions.'],
          ['Copie le jeton', 'Asana ne l’affiche qu’une fois. Colle-le ici.'],
        ],
        profile: {
          subtitle: 'arthur@acme.example',
          facts: ['Espace de travail', 'Tâches qui te sont assignées', 'Projets suivis'],
          values: ['Acme', '14', '5'],
        },
      },
      en: {
        tagline: 'Your tasks and projects',
        tokenLabel: 'Personal access token',
        tokenHelp: 'It looks like 1/123…:abc…. It stays in your Mac’s keychain and is never shown again.',
        patternHint: 'does not look like an Asana token (for example 1/1234567890:abcdef…)',
        refused: 'Asana does not accept this token (401 Not Authorized). Copy it again from your developer console.',
        linkLabel: 'Open the Asana console',
        guide: [
          ['Open your developer console', 'In Asana, go to the apps console, Personal access tokens section.'],
          ['Create a token', 'Click Create new token, name it Pouple and accept the terms.'],
          ['Copy the token', 'Asana shows it once. Paste it here.'],
        ],
        profile: {
          subtitle: 'arthur@acme.example',
          facts: ['Workspace', 'Tasks assigned to you', 'Projects followed'],
          values: ['Acme', '14', '5'],
        },
      },
    },
  },
  slite: {
    name: 'Slite',
    order: 12,
    placeholder: '…',
    pattern: '^[A-Za-z0-9]{24,}$',
    link: 'https://slite.com/api/setup',
    profileName: 'Arthur Monnet',
    text: {
      fr: {
        tagline: 'La documentation de ton équipe',
        tokenLabel: 'Clé d’API',
        tokenHelp: 'Une longue suite de lettres et de chiffres. Elle reste dans le trousseau de ton Mac et ne s’affiche plus jamais.',
        patternHint: 'ne ressemble pas à une clé Slite (24 lettres ou chiffres au moins)',
        refused: 'Slite refuse cette clé (401). Recopie-la depuis la page API de Slite.',
        linkLabel: 'Ouvrir la page API de Slite',
        guide: [
          ['Ouvre la page API', 'Dans Slite, ouvre les paramètres de ton espace puis la section API.'],
          ['Génère une clé', 'Clique sur Générer une clé d’API. Seules les personnes qui administrent l’espace le peuvent.'],
          ['Copie la clé', 'Slite ne l’affiche qu’une fois. Colle-la ici.'],
        ],
        profile: {
          subtitle: 'arthur@acme.example',
          facts: ['Espace de travail', 'Notes accessibles', 'Rôle'],
          values: ['Acme', '248', 'Membre'],
        },
      },
      en: {
        tagline: 'Your team’s documentation',
        tokenLabel: 'API key',
        tokenHelp: 'A long string of letters and digits. It stays in your Mac’s keychain and is never shown again.',
        patternHint: 'does not look like a Slite key (at least 24 letters or digits)',
        refused: 'Slite does not accept this key (401). Copy it again from the Slite API page.',
        linkLabel: 'Open the Slite API page',
        guide: [
          ['Open the API page', 'In Slite, open your workspace settings, then the API section.'],
          ['Generate a key', 'Click Generate API key. Only workspace admins can.'],
          ['Copy the key', 'Slite shows it once. Paste it here.'],
        ],
        profile: {
          subtitle: 'arthur@acme.example',
          facts: ['Workspace', 'Notes you can read', 'Role'],
          values: ['Acme', '248', 'Member'],
        },
      },
    },
  },
};

const requiredText: Record<Lang, (label: string) => string> = {
  fr: (label) => `${label} est obligatoire`,
  en: (label) => `${label} is required`,
};

export function extraConnector(id: ExtraId, lang: Lang): ConnectorStatus {
  const spec = SPECS[id];
  const text = spec.text[lang];
  const fields: Field[] = [{ id: 'token', label: text.tokenLabel, help: text.tokenHelp, placeholder: spec.placeholder, secret: true, pattern: spec.pattern, patternHint: text.patternHint }];
  if (text.host) fields.push({ id: 'baseUrl', label: text.host.label, help: text.host.help, placeholder: 'https://gitlab.example.com', secret: false, optional: true, pattern: '^https://\\S+$', patternHint: text.host.hint });
  const guide: GuideStep[] = text.guide.map(([title, body], index) => ({ title, body, ...(index === 0 && { link: { label: text.linkLabel, url: spec.link } }) }));
  return { connected: false, descriptor: { id, name: spec.name, tagline: text.tagline, required: false, order: spec.order, fields, guide } };
}

export const extraConnectors = (lang: Lang): ConnectorStatus[] => EXTRA_IDS.map((id) => extraConnector(id, lang));

export function extraProfile(id: ExtraId, lang: Lang): Profile {
  const spec = SPECS[id];
  const p = spec.text[lang].profile;
  return {
    name: spec.profileName,
    subtitle: p.subtitle,
    facts: p.facts.map((label, index) => ({ label, value: p.values[index] ?? '' })),
    warnings: p.warning ? [p.warning] : [],
  };
}

/** What the core does with the typed values: required, well-formed, then accepted by the service (or not). */
export function checkExtraValues(id: ExtraId, values: ConnectorValues, lang: Lang): void {
  const spec = SPECS[id];
  const text = spec.text[lang];
  const token = (values['token'] ?? '').trim();
  if (token === '') throw new ApiError(422, requiredText[lang](text.tokenLabel), 'token');
  if (!new RegExp(spec.pattern).test(token)) throw new ApiError(422, `${text.tokenLabel} ${text.patternHint}`, 'token');
  if (token.includes('bad')) throw new ApiError(422, text.refused, 'token');
}
