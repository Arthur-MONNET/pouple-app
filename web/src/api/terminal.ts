import { bearer, toApiError } from './client';

export interface TerminalState {
  id: string;
  dir: string;
  branch: string;
  interactive: boolean;
  command: string;
  running: boolean;
  result: { status: string; exitCode?: number; signal?: string } | null;
  revision: number;
  output: string | null;
  offset: number;
  commands: string;
  commandOffset: number;
}

export async function terminalRequest<T = TerminalState>(requestId: string, payload: object, signal: AbortSignal): Promise<T> {
  const response = await fetch(`/api/assistant/requests/${encodeURIComponent(requestId)}/terminal`, {
    method: 'POST',
    headers: { ...bearer(), 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal,
  });
  if (!response.ok) throw await toApiError(response);
  return (await response.json()) as T;
}
