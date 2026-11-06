import type { ProviderInfo, ProviderTestResult, ProvidersInfo, Settings } from './types';

const CLAUDE_MODELS = [
  { id: 'opus', label: 'Opus' },
  { id: 'sonnet', label: 'Sonnet' },
  { id: 'haiku', label: 'Haiku' },
];

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
