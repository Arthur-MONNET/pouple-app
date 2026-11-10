import type { ProviderInfo, ProviderModels, ProviderTestResult, ProvidersInfo, Settings } from './types';

const CLAUDE_MODELS = [
  { id: 'opus', label: 'Opus' },
  { id: 'sonnet', label: 'Sonnet' },
  { id: 'haiku', label: 'Haiku' },
];

const CODEX_MODELS = [
  { id: 'gpt-6.1-sol', label: 'GPT-6.1-Sol — Latest workhorse model for coding and everyday work.' },
  { id: 'gpt-6-astra', label: 'GPT-6-Astra — Frontier intelligence for the most demanding work.' },
  { id: 'gpt-6-luna', label: 'GPT-6-Luna — Fast and affordable model for easier tasks.' },
];

export function mockProviderModels(id: string): ProviderModels {
  return { models: id === 'claude' ? CLAUDE_MODELS : CODEX_MODELS, message: '' };
}

/** Both AIs are connected in the mock, so the choice can be tried. */
export function mockProviders(settings: Settings): ProvidersInfo {
  const active = settings.provider || 'codex';
  const models = settings.models ?? {};
  const providers: ProviderInfo[] = [
    { id: 'codex', name: 'Codex', connected: true, active: active === 'codex', model: models.codex ?? '', suggestedModels: null },
    { id: 'claude', name: 'Claude', connected: true, active: active === 'claude', model: models.claude ?? '', suggestedModels: CLAUDE_MODELS },
  ];
  return { active, providers };
}

export function mockProviderTest(id: string, model: string | undefined): ProviderTestResult {
  return { ok: true, latencyMs: 1200, model: model || (id === 'claude' ? 'sonnet' : 'gpt-5'), message: '' };
}
